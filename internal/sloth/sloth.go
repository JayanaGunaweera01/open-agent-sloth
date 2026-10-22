// Copyright 2026 Jayana Gunaweera (https://github.com/JayanaGunaweera01). All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package sloth

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"
)

const usage = `openagent sloth — do the boring work locally, on an open-weight model, so you don't have to.

Usage:
  openagent sloth doctor                 Check that Ollama and the model are ready
  openagent sloth digest <file|->        Two-line summary of a chat log or any text
  openagent sloth tidy <dir> [flags]     Sort and rename files (dry run unless --apply)
  openagent sloth undo <dir>             Put the last tidy back exactly as it was
  openagent sloth watch <dir> [flags]    Tidy <dir> on a slow timer, forever

tidy / watch flags:
  --apply               Actually move files (tidy only; watch always applies)
  --archive-days N      Move files untouched for N days to Archive/<year> (no model needed)
  --no-model            Sort by file type only; never contact a model
  --every DURATION      Watch interval (default 1h)

Environment:
  SLOTH_OLLAMA_URL      Ollama address (default http://localhost:11434)
  SLOTH_MODEL           Model name (default gemma4:e4b)
`

// Run executes a sloth subcommand and returns the process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	client := NewClient()

	switch args[0] {
	case "doctor":
		return doctor(ctx, client, stdout, stderr)
	case "digest":
		return digest(ctx, client, args[1:], stdin, stdout, stderr)
	case "tidy", "watch":
		return tidy(ctx, client, args[0] == "watch", args[1:], stdout, stderr)
	case "undo":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "usage: openagent sloth undo <dir>")
			return 2
		}
		n, err := Undo(args[1])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "Put %d file(s) back.\n", n)
		return 0
	}
	fmt.Fprintf(stderr, "sloth: unknown command %q\n\n%s", args[0], usage)
	return 2
}

func doctor(ctx context.Context, c *Client, stdout, stderr io.Writer) int {
	models, err := c.Models(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "%v\nStart it with: ollama serve\n", err)
		return 1
	}
	for _, m := range models {
		if m == c.Model || strings.TrimSuffix(m, ":latest") == c.Model {
			fmt.Fprintf(stdout, "Ready: Ollama at %s, model %s.\n", c.BaseURL, c.Model)
			return 0
		}
	}
	fmt.Fprintf(stderr, "Ollama is up but model %q is not pulled.\nRun: ollama pull %s\n", c.Model, c.Model)
	return 1
}

func digest(ctx context.Context, m Chatter, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: openagent sloth digest <file|->")
		return 2
	}
	var r io.Reader = stdin
	if args[0] != "-" {
		f, err := os.Open(args[0])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer f.Close()
		r = f
	}
	b, err := io.ReadAll(io.LimitReader(r, 64<<20))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	out, err := Digest(ctx, m, string(b))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(stdout, out)
	return 0
}

func tidy(ctx context.Context, c *Client, watch bool, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tidy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	apply := fs.Bool("apply", false, "actually move files")
	archive := fs.Int("archive-days", 0, "archive files untouched for N days")
	noModel := fs.Bool("no-model", false, "sort by file type only")
	every := fs.Duration("every", time.Hour, "watch interval")
	// Accept the directory before or after the flags.
	var dir string
	rest := args
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		dir, rest = rest[0], rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if dir == "" && fs.NArg() == 1 {
		dir = fs.Arg(0)
	}
	if dir == "" {
		fmt.Fprintln(stderr, "usage: openagent sloth tidy <dir> [--apply]")
		return 2
	}

	var cl Classifier = RuleClassifier{}
	if !*noModel {
		cl = LLMClassifier{Model: c, Fallback: RuleClassifier{}}
	}
	opt := Options{ArchiveDays: *archive}
	once := func() int {
		plan, err := BuildPlan(ctx, dir, cl, opt)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if len(plan) == 0 {
			fmt.Fprintln(stdout, "Already tidy. Nothing to do.")
			return 0
		}
		fmt.Fprintln(stdout, Describe(dir, plan))
		if !*apply && !watch {
			fmt.Fprintf(stdout, "\n%d file(s) would move. Re-run with --apply to do it (undo with: openagent sloth undo %s).\n", len(plan), dir)
			return 0
		}
		if err := Apply(dir, plan); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "Moved %d file(s).\n", len(plan))
		return 0
	}

	if !watch {
		return once()
	}
	fmt.Fprintf(stdout, "Watching %s every %s. Ctrl-C to stop.\n", dir, *every)
	for {
		once()
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(*every):
		}
	}
}

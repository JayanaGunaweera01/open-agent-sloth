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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const undoFile = ".sloth-undo.json"

// Categories are the only folders the model may choose from, so it can never invent
// paths. Anything it returns outside this list becomes "Misc".
var Categories = []string{"Documents", "Images", "Screenshots", "Invoices", "Code", "Data", "Archives", "Media", "Misc"}

var textExts = map[string]bool{
	".txt": true, ".md": true, ".csv": true, ".json": true, ".log": true,
	".go": true, ".py": true, ".js": true, ".ts": true, ".html": true, ".yml": true, ".yaml": true,
}

var extCategory = map[string]string{
	".pdf": "Documents", ".doc": "Documents", ".docx": "Documents", ".odt": "Documents", ".txt": "Documents", ".md": "Documents",
	".png": "Images", ".jpg": "Images", ".jpeg": "Images", ".gif": "Images", ".webp": "Images", ".svg": "Images",
	".go": "Code", ".py": "Code", ".js": "Code", ".ts": "Code", ".java": "Code", ".rs": "Code", ".html": "Code",
	".csv": "Data", ".json": "Data", ".xlsx": "Data", ".xls": "Data", ".yml": "Data", ".yaml": "Data",
	".zip": "Archives", ".tar": "Archives", ".gz": "Archives", ".7z": "Archives",
	".mp3": "Media", ".mp4": "Media", ".mov": "Media", ".mkv": "Media", ".wav": "Media",
}

// Suggestion is a classifier's opinion about one file.
type Suggestion struct {
	Category string `json:"category"`
	Name     string `json:"name"` // new base name, without extension
}

// Classifier decides where a file goes and what it is called.
type Classifier interface {
	Classify(ctx context.Context, name, snippet string) (Suggestion, error)
}

// RuleClassifier is the no-model fallback: sort by extension, keep the name.
type RuleClassifier struct{}

func (RuleClassifier) Classify(_ context.Context, name, _ string) (Suggestion, error) {
	ext := strings.ToLower(filepath.Ext(name))
	cat, ok := extCategory[ext]
	if !ok {
		cat = "Misc"
	}
	if cat == "Images" && strings.HasPrefix(strings.ToLower(name), "screenshot") {
		cat = "Screenshots"
	}
	return Suggestion{Category: cat, Name: strings.TrimSuffix(name, filepath.Ext(name))}, nil
}

// LLMClassifier asks an open-weight model, and quietly falls back to rules when the
// model is unreachable or answers nonsense, so the tidier never just gives up.
type LLMClassifier struct {
	Model    Chatter
	Fallback Classifier
}

func (c LLMClassifier) Classify(ctx context.Context, name, snippet string) (Suggestion, error) {
	sys := "You tidy files. Given a file name and an optional text snippet, reply with JSON only: " +
		`{"category": one of [` + strings.Join(Categories, ", ") + `], "name": a short descriptive lowercase-hyphenated name without extension}.`
	user := "file name: " + name
	if snippet != "" {
		user += "\nstart of file:\n" + snippet
	}
	reply, err := c.Model.Chat(ctx, sys, user, true)
	if err == nil {
		var s Suggestion
		if json.Unmarshal([]byte(reply), &s) == nil && s.Name != "" {
			return s, nil
		}
	}
	return c.Fallback.Classify(ctx, name, snippet)
}

// Move is one planned rename.
type Move struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Options tunes a tidy run.
type Options struct {
	ArchiveDays int // files untouched for this many days go to Archive/<year>; 0 disables
	Now         time.Time
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// cleanName makes a model-proposed name safe to use as a file name.
func cleanName(s, fallback string) string {
	s = strings.TrimSuffix(s, filepath.Ext(s))
	s = unsafeChars.ReplaceAllString(strings.TrimSpace(s), "-")
	s = strings.Trim(s, "-._")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-._")
	}
	if s == "" {
		return fallback
	}
	return s
}

func validCategory(c string) string {
	for _, k := range Categories {
		if strings.EqualFold(k, c) {
			return k
		}
	}
	return "Misc"
}

func readSnippet(path string) string {
	if !textExts[strings.ToLower(filepath.Ext(path))] {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 600)
	n, _ := io.ReadFull(f, buf)
	return strings.ToValidUTF8(string(buf[:n]), "")
}

// BuildPlan looks at the top level of dir (never subfolders, never hidden files) and
// proposes moves. It touches nothing on disk.
func BuildPlan(ctx context.Context, dir string, cl Classifier, opt Options) ([]Move, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	now := opt.Now
	if now.IsZero() {
		now = time.Now()
	}
	taken := map[string]bool{}
	var plan []Move
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || !e.Type().IsRegular() {
			continue
		}
		from := filepath.Join(dir, e.Name())
		ext := filepath.Ext(e.Name())
		base := strings.TrimSuffix(e.Name(), ext)

		var folder, name string
		info, ierr := e.Info()
		if opt.ArchiveDays > 0 && ierr == nil && now.Sub(info.ModTime()) > time.Duration(opt.ArchiveDays)*24*time.Hour {
			folder, name = filepath.Join("Archive", fmt.Sprint(info.ModTime().Year())), base
		} else {
			s, cerr := cl.Classify(ctx, e.Name(), readSnippet(from))
			if cerr != nil {
				return nil, cerr
			}
			folder, name = validCategory(s.Category), cleanName(s.Name, cleanName(base, "file"))
		}

		to := filepath.Join(dir, folder, name+ext)
		for i := 2; taken[to] || (to != from && exists(to)); i++ {
			to = filepath.Join(dir, folder, fmt.Sprintf("%s-%d%s", name, i, ext))
		}
		taken[to] = true
		if to != from {
			plan = append(plan, Move{From: from, To: to})
		}
	}
	return plan, nil
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// Apply performs the moves and records them so Undo can reverse the run.
func Apply(dir string, plan []Move) error {
	var done []Move
	for _, m := range plan {
		if exists(m.To) { // never overwrite, whatever happened since planning
			continue
		}
		if err := os.MkdirAll(filepath.Dir(m.To), 0o755); err != nil {
			return err
		}
		if err := os.Rename(m.From, m.To); err != nil {
			return err
		}
		done = append(done, m)
	}
	if len(done) == 0 {
		return nil
	}
	runs, _ := loadRuns(dir)
	runs = append(runs, done)
	return saveRuns(dir, runs)
}

func loadRuns(dir string) ([][]Move, error) {
	b, err := os.ReadFile(filepath.Join(dir, undoFile))
	if err != nil {
		return nil, err
	}
	var runs [][]Move
	return runs, json.Unmarshal(b, &runs)
}

func saveRuns(dir string, runs [][]Move) error {
	if len(runs) == 0 {
		return os.Remove(filepath.Join(dir, undoFile))
	}
	b, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, undoFile), b, 0o644)
}

// Undo reverses the most recent Apply and returns how many files went back.
func Undo(dir string) (int, error) {
	runs, err := loadRuns(dir)
	if err != nil || len(runs) == 0 {
		return 0, fmt.Errorf("nothing to undo in %s", dir)
	}
	last := runs[len(runs)-1]
	n := 0
	for i := len(last) - 1; i >= 0; i-- {
		m := last[i]
		if exists(m.From) || !exists(m.To) {
			continue
		}
		if err := os.Rename(m.To, m.From); err != nil {
			return n, err
		}
		n++
		removeIfEmpty(filepath.Dir(m.To), dir)
	}
	return n, saveRuns(dir, runs[:len(runs)-1])
}

// removeIfEmpty prunes folders the tidier created, climbing no higher than root.
func removeIfEmpty(d, root string) {
	for d != root && strings.HasPrefix(d, root) {
		if os.Remove(d) != nil {
			return
		}
		d = filepath.Dir(d)
	}
}

// Describe renders a plan relative to dir for humans.
func Describe(dir string, plan []Move) string {
	lines := make([]string, 0, len(plan))
	for _, m := range plan {
		a, _ := filepath.Rel(dir, m.From)
		b, _ := filepath.Rel(dir, m.To)
		lines = append(lines, fmt.Sprintf("  %s  ->  %s", a, b))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

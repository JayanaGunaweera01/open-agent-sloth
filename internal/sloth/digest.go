// Copyright 2026 Jayana Gunaweera (https://github.com/JayanaGunaweera01). All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sloth

import (
	"context"
	"fmt"
	"strings"
)

const (
	chunkChars = 6000
	maxInput   = 400000
)

const digestSystem = "You write morning digests for a busy, tired person. " +
	"Reply with exactly two lines and nothing else. " +
	"Line 1: what happened, at most 25 words. " +
	"Line 2: what needs their attention or a reply, at most 25 words; if nothing, say 'Nothing needs you.'"

// splitChunks cuts text into pieces of at most limit characters on line boundaries.
func splitChunks(text string, limit int) []string {
	var chunks []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			chunks = append(chunks, cur.String())
			cur.Reset()
		}
	}
	for _, line := range strings.Split(text, "\n") {
		for len(line) > limit { // a single giant line: hard-split it
			flush()
			chunks = append(chunks, line[:limit])
			line = line[limit:]
		}
		if cur.Len()+len(line)+1 > limit {
			flush()
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
	}
	flush()
	return chunks
}

// twoLines keeps the first two non-empty lines of a model reply.
func twoLines(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "-*•"))
		if l != "" {
			out = append(out, l)
		}
		if len(out) == 2 {
			break
		}
	}
	return strings.Join(out, "\n")
}

// Digest turns a chat log (or any long text) into a two-line summary. Long input is
// summarised chunk by chunk first, so small local models with short contexts cope.
func Digest(ctx context.Context, m Chatter, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("nothing to summarise")
	}
	if len(text) > maxInput { // keep the most recent part: that is what the morning cares about
		text = text[len(text)-maxInput:]
	}
	chunks := splitChunks(text, chunkChars)
	if len(chunks) > 1 {
		var notes []string
		for i, c := range chunks {
			note, err := m.Chat(ctx, "Condense this part of a conversation into at most 5 short bullet points. Keep names, decisions, dates and open questions.", c, false)
			if err != nil {
				return "", fmt.Errorf("chunk %d/%d: %w", i+1, len(chunks), err)
			}
			notes = append(notes, note)
		}
		text = strings.Join(notes, "\n")
	}
	reply, err := m.Chat(ctx, digestSystem, text, false)
	if err != nil {
		return "", err
	}
	res := twoLines(reply)
	if res == "" {
		return "", fmt.Errorf("model returned an empty digest")
	}
	return res, nil
}

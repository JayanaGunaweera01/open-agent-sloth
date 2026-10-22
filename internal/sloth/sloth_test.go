package sloth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeOllama answers /api/chat with whatever pick() returns for the user message.
func fakeOllama(t *testing.T, pick func(user string) string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(chatResponse{Message: chatMessage{Role: "assistant", Content: pick(req.Messages[1].Content)}})
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, Model: "test", HTTP: srv.Client()}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTidyApplyAndUndo(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "scan0001.txt", "Invoice #42 from Acme, total $90")
	write(t, dir, "IMG_9.png", "x")
	write(t, dir, ".hidden", "x")
	m := fakeOllama(t, func(user string) string {
		if strings.Contains(user, "Invoice") {
			return `{"category":"invoices","name":"Acme Invoice 42!!"}`
		}
		return `{"category":"../../etc","name":"../evil"}` // hostile output must stay inside dir
	})
	cl := LLMClassifier{Model: m, Fallback: RuleClassifier{}}

	plan, err := BuildPlan(context.Background(), dir, cl, Options{})
	if err != nil || len(plan) != 2 {
		t.Fatalf("plan=%v err=%v", plan, err)
	}
	if err := Apply(dir, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Invoices", "Acme-Invoice-42.txt")); err != nil {
		t.Fatalf("invoice not renamed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Misc", "evil.png")); err != nil {
		t.Fatalf("hostile name not contained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".hidden")); err != nil {
		t.Fatal("hidden file must be left alone")
	}
	n, err := Undo(dir)
	if err != nil || n != 2 {
		t.Fatalf("undo n=%d err=%v", n, err)
	}
	for _, f := range []string{"scan0001.txt", "IMG_9.png"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Fatalf("%s not restored", f)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Invoices")); err == nil {
		t.Fatal("empty folder should be pruned")
	}
}

func TestFallbackWhenModelDown(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "report.pdf", "x")
	dead := &Client{BaseURL: "http://127.0.0.1:1", Model: "x", HTTP: &http.Client{Timeout: time.Second}}
	plan, err := BuildPlan(context.Background(), dir, LLMClassifier{Model: dead, Fallback: RuleClassifier{}}, Options{})
	if err != nil || len(plan) != 1 || !strings.HasSuffix(plan[0].To, filepath.Join("Documents", "report.pdf")) {
		t.Fatalf("plan=%v err=%v", plan, err)
	}
}

func TestNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.txt", "1")
	write(t, dir, "b.txt", "2")
	same := fakeOllama(t, func(string) string { return `{"category":"Documents","name":"notes"}` })
	plan, _ := BuildPlan(context.Background(), dir, LLMClassifier{Model: same, Fallback: RuleClassifier{}}, Options{})
	if plan[0].To == plan[1].To {
		t.Fatal("two files planned onto the same target")
	}
}

func TestArchiveOldFilesWithoutModel(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "old.log", "x")
	old := time.Now().AddDate(-1, 0, 0)
	_ = os.Chtimes(filepath.Join(dir, "old.log"), old, old)
	plan, err := BuildPlan(context.Background(), dir, RuleClassifier{}, Options{ArchiveDays: 30})
	if err != nil || len(plan) != 1 || !strings.Contains(plan[0].To, "Archive") {
		t.Fatalf("plan=%v err=%v", plan, err)
	}
}

func TestDigestChunksLongInput(t *testing.T) {
	calls := 0
	m := fakeOllama(t, func(user string) string {
		calls++
		return "- Plans changed to Friday.\n- Sam needs the deck by noon.\n- extra line"
	})
	long := strings.Repeat("alex: are we still on for thursday?\n", 800)
	out, err := Digest(context.Background(), m, long)
	if err != nil {
		t.Fatal(err)
	}
	if calls < 3 {
		t.Fatalf("expected chunk calls plus a final call, got %d", calls)
	}
	if got := strings.Count(out, "\n") + 1; got != 2 {
		t.Fatalf("want 2 lines, got %d: %q", got, out)
	}
}

func TestRunCommands(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Run([]string{"help"}, nil, &out, &errb); code != 0 || !strings.Contains(out.String(), "sloth") {
		t.Fatal("help failed")
	}
	dir := t.TempDir()
	write(t, dir, "x.png", "x")
	out.Reset()
	if code := Run([]string{"tidy", dir, "--no-model"}, nil, &out, &errb); code != 0 || !strings.Contains(out.String(), "would move") {
		t.Fatalf("dry run: %s %s", out.String(), errb.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "x.png")); err != nil {
		t.Fatal("dry run must not move files")
	}
	if code := Run([]string{"nope"}, nil, &out, &errb); code != 2 {
		t.Fatal("unknown command should exit 2")
	}
}

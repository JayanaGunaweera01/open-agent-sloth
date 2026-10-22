# 🦥 OpenAgent Sloth Mode

Built for the Hacktoberfest Open-Source AI Challenge Week 3 (Slow and Steady): an open-source agent harness plus an
open-weight model, doing the dull work locally so you don't have to.

`openagent sloth` is a small, stdlib-only toolkit inside OpenAgent. It needs no database,
no login, no cloud key and no network beyond `localhost`. The open pieces are the whole point:
**OpenAgent** (Apache-2.0) as the harness and **an open-weight model served by Ollama**
(default `gemma4:e4b`; any model Ollama can run works).

## Quick start

```bash
ollama pull gemma4:e4b          # one-time
openagent sloth doctor           # checks Ollama + model are ready

# 1. Tidy a messy folder. Dry run by default, undoable.
openagent sloth tidy ~/Downloads
openagent sloth tidy ~/Downloads --apply --archive-days 90
openagent sloth undo ~/Downloads

# 2. Two-line morning digest of any chat export or text
openagent sloth digest overnight-group-chat.txt
cat chat.txt | openagent sloth digest -

# 3. Be maximally lazy: tidy on a slow timer, forever
openagent sloth watch ~/Downloads --every 6h
```

Example digest output:

```
Dinner moved to Friday 7pm at Sam's; Priya is bringing dessert.
Reply to Alex about the venue deposit before noon.
```

## Why it is safe to leave unattended

- **Dry run first.** `tidy` only prints a plan until you pass `--apply`.
- **Undo.** Every applied run is logged in `.sloth-undo.json`; `undo` restores the last one.
- **Never overwrites** a file, and never touches hidden files or subfolders.
- **The model can't choose paths.** It picks from a fixed category list and proposes a name that
  is sanitised to `[A-Za-z0-9._-]`, so a confused or hostile reply stays inside the folder.
- **Works without the model.** If Ollama is down, tidying falls back to sorting by file type.
  `--no-model` forces that, and `--archive-days` never needs a model at all.
- **Small-model friendly.** Long chats are condensed chunk by chunk, so small (e4b-class) models with
  short contexts cope.

## Config

| Variable | Default |
|---|---|
| `SLOTH_OLLAMA_URL` | `http://localhost:11434` |
| `SLOTH_MODEL` | `gemma4:e4b` |

## Code map

- `internal/sloth/ollama.go` – Ollama client (`/api/chat`, `/api/tags`)
- `internal/sloth/tidy.go` – plan / apply / undo, rule and LLM classifiers
- `internal/sloth/digest.go` – chunked two-line digest
- `internal/sloth/sloth.go` – subcommands
- `internal/cli/cli.go` – registers `openagent sloth`
- Tests: `go test ./internal/sloth/` (uses a fake Ollama server; no model needed)

## Author

Sloth Mode by [Jayana Gunaweera](https://github.com/JayanaGunaweera01).

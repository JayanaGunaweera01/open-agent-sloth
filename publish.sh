#!/usr/bin/env bash
# Publishes this project to GitHub, automating everything EXCEPT merging:
#   - commits the OpenAgent snapshot to main
#   - creates 5 stacked feature branches (one commit per logical piece), pushes them
#   - opens one pull request per branch
# You merge the PRs yourself, in order (see the printed instructions at the end).
# Commits use your git identity and the real current time.
#
# Usage (from the extracted project folder, e.g. openagent-master/):
#   bash ../publish.sh
# Optional: REMOTE=<url> to push somewhere else; FORCE=1 to overwrite existing history (use after reset-repo.sh).
set -euo pipefail

REMOTE="${REMOTE:-https://github.com/JayanaGunaweera01/open-agent-sloth.git}"

[[ -f go.mod && -d internal/sloth ]] || { echo "Run this from the project root (the folder with go.mod)."; exit 1; }
[[ -e .git ]] && { echo "A .git folder already exists here. Remove it or use a fresh copy."; exit 1; }
git config user.name  >/dev/null || { echo "Set your identity first: git config --global user.name \"Your Name\""; exit 1; }
git config user.email >/dev/null || { echo "Set your identity first: git config --global user.email \"you@example.com\""; exit 1; }

# 1. Base commit on main: the OpenAgent snapshot without the Sloth feature.
BACKUP="$(mktemp -d)"
cp internal/cli/cli.go "$BACKUP/cli.go"
cp -R internal/sloth "$BACKUP/sloth"
cp SLOTH.md "$BACKUP/SLOTH.md"

sed -i '/internal\/sloth"/d; /case "sloth":/,+2d; /%\[1\]s sloth /d' internal/cli/cli.go
rm -rf internal/sloth SLOTH.md

git init -q -b main
git add -A
git commit -q -m "chore: import OpenAgent snapshot (Apache-2.0)" \
  -m "Starting point for this project. Upstream: https://github.com/the-open-agent/openagent"

# 2. Stacked branches: each starts from the previous one, so every PR builds on the last.
mkdir -p internal/sloth
BRANCHES=()
PR_TITLES=()
PR_BODIES=()
PREV=main

start_branch() { # <branch> <PR title> <PR body>
  git checkout -q -b "$1" "$PREV"
  BRANCHES+=("$1"); PR_TITLES+=("$2"); PR_BODIES+=("$3"); PREV="$1"
}
commit_file() { # <path> <message>
  git add "$1"; git commit -q -m "$2"
}

start_branch feat/sloth-ollama-client \
  "feat(sloth): Ollama client with gemma4:e4b default" \
  "Adds a small standard-library-only client for a local Ollama server. Default model is \`gemma4:e4b\`; override with \`SLOTH_MODEL\` / \`SLOTH_OLLAMA_URL\`."
cp "$BACKUP/sloth/ollama.go" internal/sloth/ollama.go
commit_file internal/sloth/ollama.go "feat(sloth): add stdlib Ollama client with gemma4:e4b default"

start_branch feat/sloth-tidy \
  "feat(sloth): folder tidy planner with undo" \
  "Adds the tidy planner. The model only picks from a fixed category list and file names are sanitised, so a confused reply cannot escape the folder. Never overwrites files, and every applied run is logged for undo. Falls back to sorting by file type when the model is unavailable."
cp "$BACKUP/sloth/tidy.go" internal/sloth/tidy.go
commit_file internal/sloth/tidy.go "feat(sloth): add folder tidy planner with sanitised names and undo log"

start_branch feat/sloth-digest \
  "feat(sloth): chunked chat digest" \
  "Adds a two-line digest for chat exports and long text. Long inputs are condensed chunk by chunk so small-context local models can handle them."
cp "$BACKUP/sloth/digest.go" internal/sloth/digest.go
commit_file internal/sloth/digest.go "feat(sloth): add chunked chat digest for small-context models"

start_branch feat/sloth-cli \
  "feat(sloth): tidy, digest, watch, undo and doctor commands" \
  "Adds the \`sloth\` subcommands and dispatches them from the CLI before server startup, so no database or login is needed."
cp "$BACKUP/sloth/sloth.go" internal/sloth/sloth.go
commit_file internal/sloth/sloth.go "feat(sloth): add tidy, digest, watch, undo and doctor subcommands"
cp "$BACKUP/cli.go" internal/cli/cli.go
commit_file internal/cli/cli.go "feat(cli): dispatch 'sloth' before server startup"

start_branch feat/sloth-tests-docs \
  "test+docs(sloth): unit tests and usage guide" \
  "Adds unit tests (fake Ollama server, no model needed: \`go test ./internal/sloth/\`), and the usage guide SLOTH.md."
cp "$BACKUP/sloth/sloth_test.go" internal/sloth/sloth_test.go
commit_file internal/sloth/sloth_test.go "test(sloth): cover hostile model output, collisions, undo and offline fallback"
cp "$BACKUP/SLOTH.md" SLOTH.md
commit_file SLOTH.md "docs(sloth): add usage guide, env vars and author credit"

rm -rf "$BACKUP"

# Sanity check before anything leaves your machine.
if command -v go >/dev/null; then
  echo "Running Sloth tests..."
  go test ./internal/sloth/
fi

# 3. Push main and every branch (nothing is merged).
git remote add origin "$REMOTE"
PUSHFLAG=""; [[ "${FORCE:-0}" == "1" ]] && PUSHFLAG="--force"   # FORCE=1 overwrites an existing remote history
git push $PUSHFLAG -u origin main
for b in "${BRANCHES[@]}"; do git push $PUSHFLAG -u origin "$b"; done

# 4. Open one PR per branch, each targeting the branch before it.
BASE=main
for i in "${!BRANCHES[@]}"; do
  HEAD="${BRANCHES[$i]}"
  if command -v gh >/dev/null; then
    gh pr create --base "$BASE" --head "$HEAD" --title "${PR_TITLES[$i]}" --body "${PR_BODIES[$i]}" \
      || echo "PR for $HEAD failed; re-run create-missing-prs.sh afterwards."
    sleep 3   # avoid GitHub's secondary rate limit on rapid PR creation
  else
    echo "Open PR $((i+1)) in your browser: ${REMOTE%.git}/compare/$BASE...$HEAD?expand=1"
  fi
  BASE="$HEAD"
done

cat <<MSG

Done. Nothing has been merged. Merge the PRs yourself, in this order:
  1. feat/sloth-ollama-client   -> main
  2. feat/sloth-tidy            -> main
  3. feat/sloth-digest          -> main
  4. feat/sloth-cli             -> main
  5. feat/sloth-tests-docs      -> main
Tips:
  - Merge PR 1 first and tick "Delete branch": GitHub then retargets PR 2 to main automatically. Repeat for each PR.
  - Use "Create a merge commit" (not squash) so the stacked branches stay in sync.
MSG

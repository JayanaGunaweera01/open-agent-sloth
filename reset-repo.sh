#!/usr/bin/env bash
# Closes all open PRs and deletes all non-default branches of the repo (the repo itself stays).
# Needs the GitHub CLI, logged in with repo access:  gh auth login
# Usage: bash reset-repo.sh          (add YES=1 to skip the confirmation prompt)
set -u
REPO="${REPO:-JayanaGunaweera01/open-agent-sloth}"

command -v gh >/dev/null || { echo "ERROR: GitHub CLI (gh) not found. Install: https://cli.github.com"; exit 1; }
gh auth status >/dev/null 2>&1 || { echo "ERROR: not logged in. Run: gh auth login"; exit 1; }
gh repo view "$REPO" >/dev/null 2>&1 || { echo "ERROR: cannot access $REPO. Check the name and that your login has access."; gh repo view "$REPO"; exit 1; }

if [[ "${YES:-0}" != "1" ]]; then
  echo "This will CLOSE all open PRs and DELETE all branches except the default in $REPO."
  read -r -p "Type the repo name ($REPO) to confirm: " ans
  [[ "$ans" == "$REPO" ]] || { echo "Cancelled."; exit 1; }
fi

fail=0
echo "== Closing open PRs"
for n in $(gh pr list --repo "$REPO" --state open --limit 200 --json number --jq '.[].number'); do
  echo "closing PR #$n"
  gh pr close "$n" --repo "$REPO" || { echo "  could not close #$n"; fail=1; }
done

DEFAULT="$(gh repo view "$REPO" --json defaultBranchRef --jq .defaultBranchRef.name)"
echo "== Deleting branches (keeping default: $DEFAULT)"
for b in $(gh api "repos/$REPO/branches" --paginate --jq '.[].name'); do
  [[ "$b" == "$DEFAULT" ]] && continue
  echo "deleting $b"
  gh api -X DELETE "repos/$REPO/git/refs/heads/$b" || { echo "  could not delete $b"; fail=1; }
done

if [[ $fail -eq 0 ]]; then
  echo "Done. Now run from the project folder:  FORCE=1 bash ../publish.sh"
else
  echo "Finished with errors (see above). You can still run: FORCE=1 bash ../publish.sh"
fi

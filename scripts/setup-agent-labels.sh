#!/usr/bin/env sh
# Create missing labels only; requires gh authentication and label management permission.
set -eu
repo=AllenMuu/agent-manager
command -v gh >/dev/null 2>&1 || { echo "Install and authenticate GitHub CLI first." >&2; exit 1; }
gh auth status >/dev/null
# A failed listing must stop rather than be mistaken for an absent label.
labels=$(gh label list --repo "$repo" --limit 1000 --json name --jq '.[].name')
create_if_missing() {
  name=$1 color=$2 description=$3
  if printf '%s\n' "$labels" | grep -Fx -- "$name" >/dev/null; then
    printf 'Exists: %s\n' "$name"
  else
    gh label create "$name" --repo "$repo" --color "$color" --description "$description"
  fi
}
create_if_missing agent:ready 0E8A16 'Ready for one agent after human triage'
create_if_missing agent:in-progress FBCA04 'One agent is working on this issue'
create_if_missing agent:blocked D93F0B 'Blocked pending a human decision or dependency'
create_if_missing agent:review 1D76DB 'Draft PR awaiting human review'

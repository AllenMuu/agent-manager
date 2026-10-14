## Why

Developers using multiple coding agents currently accumulate a large, undifferentiated set of skills in agent-wide directories, even when most projects do not need them. Skill Manager provides one local source of truth and lets each project activate only the skills it needs without losing its independent project skills.

## What Changes

- Add a Go CLI that treats `~/.agent-manager/skills` as the default configurable Skill repository and manages directory Skills containing `SKILL.md`.
- Add searchable, scriptable, and interactive terminal skill selection, with target-agent selection for Claude Code and Codex.
- Add project skill lifecycle operations: activate, list, remove, adopt existing project skills, and fork managed skills back to independent project skills.
- Add explicit global initialization that verifies the CLI and installs a minimal Operator skill for Claude Code and Codex.
- Add guarded file operations with previews, confirmation, operation journaling, undo, diagnostics, reconciliation, Git guidance, and conflict protection.
- Establish an adapter boundary so additional agents can be detected and added later without claiming semantic compatibility.

## Capabilities

### New Capabilities

- `skill-catalog`: Discover, validate, search, and describe directory skills in the configured skill library.
- `project-skill-lifecycle`: Activate, remove, adopt, fork, and enumerate managed and unmanaged project skills.
- `agent-adapters-and-initialization`: Place skills for supported target agents and maintain the minimal global Operator-skill baseline.
- `guarded-operations-and-diagnostics`: Preview and protect filesystem changes, journal and undo operations, and diagnose or repair local activations.

### Modified Capabilities

- None.

## Impact

- New Go CLI, configuration/state storage, interactive terminal UI, and filesystem operation layer.
- Reads and creates links in the configured skill library plus Claude Code and Codex global/project skill locations.
- Adds a minimal global Operator skill only through the explicit `init` command.
- No network access, dependency installation, or execution of code contained in managed skills.
- Keeps domain behavior independent from terminal delivery so later React WebUI and Wails macOS clients can reuse the Go core.

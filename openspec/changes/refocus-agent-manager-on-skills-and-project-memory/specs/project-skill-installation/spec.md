## ADDED Requirements

### Requirement: Select an addressable Skill version for activation
An installation request SHALL resolve the explicitly requested declared version or repository key when provided. If no version is requested, it SHALL bind to the identity's current version at preview time. The preview and resulting agent-facing link SHALL refer to the exact resolved version directory; adding or selecting a version SHALL NOT overwrite or remove other stored versions.

#### Scenario: Install an explicitly selected version
- **WHEN** an operator runs `agent-manager install go-helper --version go-helper=1.2.0 --project . --target codex --yes`
- **THEN** the confirmed plan links Codex to the repository directory for version `1.2.0`

#### Scenario: Default selection is bound at preview
- **WHEN** an install request omits a version selector
- **THEN** Agent Manager records the current repository version key in the preview and reuses that exact key when applying the plan

#### Scenario: A retained version is removed from an activation
- **WHEN** a project link to a retained repository version is removed
- **THEN** only the agent-facing link is removed and all repository versions remain available

### Requirement: Preserve guarded Global and Project activation scopes
The CLI SHALL support explicit Global and Project Skill activation scopes. Global activation SHALL use only the current user's home directory and the selected target adapter's global Skill location. `--global` SHALL NOT be combined with an explicit `--project`; both scopes SHALL use the same preview, confirmation, exact-version binding, journal, rollback, and removal-safety guarantees. Global list, remove, and undo operations SHALL address only global links. The WebUI installation surface remains project-scoped.

#### Scenario: Global installation previews every selected target path
- **WHEN** an operator runs `agent-manager install go-helper --global --target claude-code --target codex --target pi`
- **THEN** the plan lists each adapter's home-level Skill path and makes no changes before confirmation

#### Scenario: Global removal preserves repository versions
- **WHEN** an operator removes a managed global activation
- **THEN** Agent Manager removes only the selected agent-facing link and retains every repository version

#### Scenario: Global scope cannot target an arbitrary project path
- **WHEN** an operator supplies both `--global` and `--project`
- **THEN** Agent Manager rejects the request without changing either path

#### Scenario: Global undo is isolated from project operations rooted at home
- **WHEN** project operations use the user's home directory as their project root and the operator runs `undo --global`
- **THEN** only the latest global journal entry is considered for restoration

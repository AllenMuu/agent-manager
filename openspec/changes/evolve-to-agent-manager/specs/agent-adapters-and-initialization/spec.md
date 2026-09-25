## MODIFIED Requirements

### Requirement: Supported target-agent adapters
The system SHALL provide capability-declaring adapters for Claude Code, Codex, and Pi. An adapter SHALL guarantee placement only for resource kinds it declares as supported and SHALL NOT claim that resource instruction content is semantically compatible.

#### Scenario: Supported Skill target is selected
- **WHEN** the user activates a Skill for a supported target agent in a project
- **THEN** its adapter places the managed link in that agent's recognized project Skill location

#### Scenario: Unsupported resource kind is targeted
- **WHEN** the user targets an adapter that does not support the requested resource kind
- **THEN** the system reports the target capability mismatch and makes no filesystem change

#### Scenario: Pi SubAgent placement is requested
- **WHEN** a user targets a canonical SubAgent at the Pi adapter
- **THEN** the system reports an explicit unsupported representation because no verified native Pi SubAgent format exists, and makes no filesystem change

### Requirement: Explicit global initialization
The system SHALL modify the global baseline only through an explicit initialization command. Initialization SHALL verify CLI availability and install or update a minimal Operator skill only for supported global agent adapters.

#### Scenario: Project activation runs after initialization
- **WHEN** a user activates Skills for a project
- **THEN** the command does not modify global agent Skill locations

### Requirement: Primary Agent Manager CLI with compatibility alias
The system SHALL expose `agent-manager` as the primary CLI and SHALL keep `skill-manager` as a compatibility alias that communicates the migration path. Both entrypoints SHALL preserve existing Skill-management behavior during the compatibility period.

#### Scenario: Existing automation invokes skill-manager
- **WHEN** a user invokes the compatibility command for an existing Skill workflow
- **THEN** the command completes with equivalent behavior and presents a deprecation notice directing the user to `agent-manager`

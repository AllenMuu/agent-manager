# Agent Manager product roadmap

Agent Manager focuses on project-aware Skills and shared Project Memory across coding agents. It manages portable knowledge and guarded local links; it does not launch agents or take ownership of their runtime, model, provider, or sandbox.

## Sequence

1. **Skill repository core — current priority.** Store Skills in the versioned local repository at `~/.agent-manager/skills`; report version and provenance; copy explicitly selected Skills from `~/.agents/skills` only after preview and confirmation; resolve one selected version for a guarded Global or Project activation. Project activation remains machine-local and derived from links, without a committed manifest. Recommendations remain read-only.
2. **Project Memory — following phase.** Continue the Skill-first sequence, then expand shared Project Memory workflows across coding agents. Preserve the existing Codex/Pi shared-memory capability while the broader integration is developed.
3. **Further Skill health and effective-set workflows.** Consider richer compatibility and health views after the repository, version selection, import, and activation contracts are stable.

## Boundaries

Existing CLI, Web console, agent adapters, and local control-plane records remain available when they directly support Skill or Project Memory work. New work on execution, orchestration, checkpointing, model/provider management, or runtime enforcement requires a separate product decision. Skill discovery, recommendation, import, and activation never execute Skill content or fetch remote Skills.

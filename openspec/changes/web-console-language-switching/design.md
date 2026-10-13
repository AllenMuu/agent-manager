## Context

See `proposal.md` and `specs/web-console-language-switching/spec.md`. The main console is a React application in `web/src/`, embedded by the Go `internal/webconsole` server. The separate local Skill installation page is a React application in `web/install/`, embedded by `internal/webui`. Neither application has an existing localization dependency. The main console server already returns stable codes for API errors, while project diagnostics and operation-plan fields currently include plain English strings.

## Goals / Non-Goals

**Goals:**
- Keep language selection and translated presentation in the browser client.
- Give fixed server-generated messages stable presentation identifiers and parameters while preserving raw text for fallback.
- Keep switching language independent from view and form state.

**Non-Goals:**
- Translate Skill or SubAgent source documents, project names, paths, identifiers, or other local source content.
- Localize the CLI, logs, or server-generated console output.
- Add a third-party localization package or make server responses depend on the browser's language.

## Decisions

### Keep the locale catalog in the React console

Add a small typed language context and local message catalog for `en` and `zh-CN`. Use English source copy as the fallback when a translation is missing. Detect the browser's preferred language only when no valid saved choice exists; persist explicit choices under a namespaced `localStorage` key. Update the document `lang` attribute and format timestamps with the selected locale. The language control updates context without remounting the current page.

An external i18n package is unnecessary for two local languages and would add a frontend dependency. Server-side locale negotiation would couple API behavior to presentation and make language preference harder to keep browser-local.

The installation page uses a small local message catalog alongside its React entry point. It detects Chinese browser language on first visit, saves a manual choice under its own local-storage key, updates the document language, and translates fixed installation-plan labels. It preserves dynamic skill content and paths. Known fixed server messages are translated by exact message or a narrow interpolation pattern; unknown server text remains visible verbatim.

The optional `agent-manager web --with-install-ui --project <path>` mode starts both existing loopback servers on their usual dynamically assigned ports. A process-local link holder passes each generated URL to the other page for a header link; it is not written to disk. The console entry URL keeps its session token in the URL fragment. Without the option, both commands retain their independent startup behavior.

### Give generated console messages stable presentation identifiers

Keep API error codes as the error identifier. Add optional message identifiers and interpolation parameters to Web console DTOs for fixed diagnostic messages and operation-plan headings, actions, and warnings. Preserve the existing English message fields and all dynamic source values; the client uses a translation when the identifier is known and otherwise displays the original message. Identifiers are language-neutral and the Go domain continues to produce locale-independent data.

Matching on complete English sentences was rejected because copy edits would silently break translations. Translating server responses in Go was rejected because the console is the only affected presentation client and locale must not alter domain or API behavior.

### Localize fixed state values while preserving source content

Translate known finite status, availability, operation, target, and capability labels at the presentation boundary. Keep Skill and SubAgent names, descriptions and instructions, project and resource paths, identifiers, and free-form diagnostic details verbatim. This lets the interface be Chinese while retaining exact values needed to identify local resources and review changes.

## Risks / Trade-offs

- [A fixed message is added without a Chinese catalog entry] → Fall back to its English source text and keep the raw message visible; keep the message catalog and API identifiers adjacent in the implementation.
- [Browser storage is unavailable or contains an invalid value] → Ignore invalid stored values, use browser-language detection for the session, and keep manual changes active in memory.
- [Text expansion changes responsive layout] → Keep the language control usable at desktop and narrow mobile widths and allow translated labels to wrap where needed.

## Migration Plan

No persistent data migration is required. Deploy the updated embedded console assets and additive, optional DTO fields together. Existing clients can continue reading the original message fields; the updated console falls back to them for unknown identifiers. Reverting the binary restores the English-only console without changing project data.

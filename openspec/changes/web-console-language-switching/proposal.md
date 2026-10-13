## Why

The local Web console currently presents its built-in interface only in English, which makes it harder for Chinese-speaking operators to review project state and guarded operations. Adding a persistent English/Simplified Chinese choice makes the existing console usable in either language without changing project or Skill library content.

## What Changes

- Add an English/Simplified Chinese language control to the main Web console.
- Detect the browser language on first visit, remember an operator's manual choice in that browser, and keep the current view and form state when switching languages.
- Translate console-owned interface copy and fixed Agent Manager messages, including errors, diagnostics, status labels, and operation-plan actions and warnings.
- Keep source content and unknown dynamic details verbatim, including Skill content, names, identifiers, and filesystem paths.

## Capabilities

### New Capabilities
- `web-console-language-switching`: Select and persist the main Web console language and present console-owned text in English or Simplified Chinese.

### Modified Capabilities

## Impact

- Affected code: `web/src/` and `internal/webconsole/` response DTOs for stable identifiers on generated messages.
- API: Add optional stable message identifiers for fixed diagnostics and operation-plan text; keep existing raw messages and dynamic content available for fallback and troubleshooting.
- Systems: Browser-local language preference only; no runtime network access, shared preference, or changes to the separate `web/install` UI.

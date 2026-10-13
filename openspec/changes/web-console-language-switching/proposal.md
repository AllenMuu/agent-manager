## Why

The local Web console and the local Skill installation page currently present their built-in interfaces only in English, which makes them harder for Chinese-speaking operators to use. Adding an English/Simplified Chinese choice makes both Web interfaces usable in either language without changing project or Skill library content.

## What Changes

- Add an English/Simplified Chinese language control to the main Web console and local Skill installation page.
- Serve the management dashboard and Skill installation page as tabs in one local Web application, using one loopback address and one CLI startup command.
- Detect the browser language on first visit, remember one shared language choice across the Web application, and keep the current view and form state when switching languages.
- Translate Web-owned interface copy and fixed Agent Manager messages, including errors, diagnostics, status labels, and operation-plan actions and warnings.
- Keep source content and unknown dynamic details verbatim, including Skill content, names, identifiers, and filesystem paths.

## Capabilities

### New Capabilities
- `web-console-language-switching`: Select and persist the language for both local Web interfaces and present Web-owned text in English or Simplified Chinese.

### Modified Capabilities

## Impact

- Affected code: `web/src/`, `internal/webconsole/` response DTOs for stable identifiers on generated messages, and `web/install/` with embedded assets in `internal/webui/dist`.
- API: Add optional stable message identifiers for main-console diagnostics and operation-plan text; keep existing raw messages and dynamic content available for fallback and troubleshooting. The installation page needs no API changes.
- Systems: Browser-local language preference is shared by all tabs. Both clients use the same loopback origin and session; no runtime network access is added.

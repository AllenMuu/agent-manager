## 1. Message presentation contract

- [x] 1.1 Add language-neutral identifiers and interpolation parameters for fixed Agent Manager diagnostics, recommendation rationales, and guarded-operation review text in the Web console response DTOs, preserving the existing raw text as fallback; verify the Go Web console package builds.

## 2. Language preference and presentation

- [x] 2.1 Add English and Simplified Chinese catalogs, first-visit browser-language detection, browser-local persistence for explicit selections, and document-language updates; verify the console frontend builds.
- [x] 2.2 Add the language control without resetting the current view or pending state, localize all built-in main-console copy and fixed generated messages, and format timestamps using the selected language; verify the console frontend builds.
- [x] 2.3 Preserve project and Skill library source values and fall back to original text for unknown message identifiers; verify with the TypeScript/Vite production build.

## 3. Embedded delivery and specification

- [x] 3.1 Rebuild the embedded main-console assets and confirm the generated entry point includes the language catalog and switch.
- [x] 3.2 Validate the OpenSpec change and run the repository's OpenSpec relationship check.

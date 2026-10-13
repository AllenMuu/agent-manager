## Purpose

Lets operators use the main local Web console in English or Simplified Chinese while preserving the original language and exact values of project and Skill library content.

## ADDED Requirements

### Requirement: Select and remember the console language
The console SHALL support English (`en`) and Simplified Chinese (`zh-CN`). On a browser's first visit, it SHALL select Simplified Chinese when the browser's preferred language is Chinese and English otherwise. An operator SHALL be able to change the language without leaving the current view or losing current form and review state. The console SHALL remember an explicit selection in that browser, and that selection SHALL take precedence over later browser-language detection.

#### Scenario: Choose a language on first visit
- **WHEN** no console-language preference has been saved in the browser
- **THEN** the console selects Simplified Chinese for a Chinese browser language and English for other browser languages

#### Scenario: Restore an explicit language choice
- **WHEN** the operator previously selected a language in this browser and opens the console again
- **THEN** the console uses that saved language regardless of the browser's current preferred language

#### Scenario: Change language during a review
- **WHEN** the operator changes the console language while viewing a project, editing a search, or reviewing a guarded operation plan
- **THEN** the current view, entered values, selected items, and pending review remain available in the newly selected language

### Requirement: Localize console-owned text and generated messages
The console SHALL present its built-in navigation, labels, instructions, loading and empty states, notifications, errors, status labels, diagnostics, and guarded-operation review text in the selected language. Agent Manager APIs SHALL expose stable identifiers for fixed generated diagnostics and operation-review messages so the console can localize them without matching English prose. Error responses SHALL continue to expose their stable error code. The document language and locale-sensitive date and time formatting SHALL follow the selected console language.

#### Scenario: Read the console in Simplified Chinese
- **WHEN** Simplified Chinese is selected
- **THEN** all built-in console text, supported system statuses, and fixed Agent Manager-generated diagnostic and operation-review messages are presented in Chinese

#### Scenario: Show a known API error
- **WHEN** an API request fails with a recognized error code
- **THEN** the console displays the error in the selected language while retaining any dynamic detail needed to understand the failure

#### Scenario: Show an unknown generated message
- **WHEN** the console receives an unknown message identifier or dynamic message detail for which it has no translation
- **THEN** the console preserves and displays the original text rather than dropping the diagnostic information

#### Scenario: Format a timestamp
- **WHEN** the console displays an operation timestamp
- **THEN** the date and time use the formatting convention for the selected console language

### Requirement: Preserve local source content
Changing the console language SHALL NOT translate, rewrite, or normalize source content returned from the registered project or configured Skill library. This includes project and Skill names, identifiers, descriptions, Skill bodies, tags, compatibility values, and filesystem paths. Dynamic technical values SHALL remain exact when included in a localized generated message.

#### Scenario: Display a Skill with source-language content
- **WHEN** the operator views a Skill whose name, description, or body is in a language different from the selected console language
- **THEN** the console displays that source content unchanged while its surrounding interface uses the selected language

#### Scenario: Display project paths and identifiers
- **WHEN** the console displays a project path, resource identifier, or technical value inside a translated status or diagnostic
- **THEN** the value remains byte-for-byte unchanged

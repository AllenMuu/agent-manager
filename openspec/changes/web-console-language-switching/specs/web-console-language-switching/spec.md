## Purpose

Lets operators use the local Web console and Skill installation page in English or Simplified Chinese while preserving the original language and exact values of project and Skill library content.

## ADDED Requirements

### Requirement: Select and remember the Web interface language
The Web application SHALL support English (`en`) and Simplified Chinese (`zh-CN`) across its management and Skill installation tabs. On a browser's first visit, it SHALL select Simplified Chinese when the browser's preferred language is Chinese and English otherwise. An operator SHALL be able to change the language without leaving the current view or losing current form, selection, and review state. The application SHALL remember one explicit language selection in that browser, and that selection SHALL take precedence over later browser-language detection in every tab.

#### Scenario: Choose a language on first visit
- **WHEN** no language preference has been saved for the opened Web interface
- **THEN** the opened Web interface selects Simplified Chinese for a Chinese browser language and English for other browser languages

#### Scenario: Restore an explicit language choice
- **WHEN** the operator previously selected a language for this Web interface and opens it again
- **THEN** that Web interface uses its saved language regardless of the browser's current preferred language

#### Scenario: Change language during a review
- **WHEN** the operator changes the language while viewing a project, editing a search, selecting Skills, or reviewing an installation or guarded operation plan
- **THEN** the current view, entered values, selected items, and pending review remain available in the newly selected language

### Requirement: Localize Web-owned text and generated messages
The management and Skill installation tabs SHALL present their built-in labels, instructions, loading and empty states, notifications, and errors in the selected language. The management tab SHALL also localize status labels, diagnostics, and guarded-operation review text. Agent Manager management APIs SHALL expose stable identifiers for fixed generated diagnostics and operation-review messages so the client can localize them without matching English prose. Error responses SHALL continue to expose their stable error code. The document language and locale-sensitive date and time formatting SHALL follow the selected language. The installation tab SHALL translate known fixed installation messages and SHALL preserve unknown server text verbatim.

#### Scenario: Read the console in Simplified Chinese
- **WHEN** Simplified Chinese is selected
- **THEN** all built-in text in the selected Web interface is presented in Chinese, along with the main console's supported system statuses and fixed Agent Manager-generated diagnostic and operation-review messages

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

### Requirement: Use one Web application for management and installation
The `agent-manager web --project <path>` command SHALL start the management dashboard and Skill installation tab on one IPv4 loopback listener and SHALL print exactly one entry URL. The management dashboard SHALL open by default. The Skill installation tab SHALL use the same origin and authenticated session as the dashboard. The CLI SHALL NOT expose a separate command for starting the installation UI.

#### Scenario: Start the local Web application
- **WHEN** the operator runs `agent-manager web --project <path>`
- **THEN** one local listener serves the management dashboard and Skill installation tab, the dashboard opens first, and the command prints one URL

#### Scenario: Open Skill installation
- **WHEN** the operator selects the Skill installation tab
- **THEN** it loads from the same Web application and uses the active console session for catalog, recommendation, preview, and apply requests

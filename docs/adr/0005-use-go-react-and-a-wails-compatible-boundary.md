# Use Go for the core and CLI, with a React WebUI and Wails-compatible boundary

Agent Manager uses Go for its domain model, application services, filesystem operations, and operation journal; those remain independent from command, terminal, and browser presentation packages.

The CLI and React Web application use the same Go application services. The Web application is served by one Agent Manager process on an IPv4 loopback address and embeds one compiled React console client in the Go executable. The management dashboard opens by default, and its single Skills workspace combines project recommendations, eligible catalog browsing, Skill details, and the existing guarded activation and batch-installation flows. The `/install/` path remains as a compatibility entry to that console with Skills selected; it does not serve a second client. The console and its API share the same origin and session-scoped authentication. The Web application has one CLI entry point (`agent-manager web`) and one printed URL. It uses guarded local services and is not a public HTTP service or general-purpose remote API. A Wails macOS package may later reuse the React interface and Go core.

## Considered Options

- Use Node.js and TypeScript for the CLI, service, and Web UI
- Use Go for the CLI and core with React for the Web UI, preserving a Wails-compatible boundary
- Use SwiftUI and AppKit for a macOS-only application

The Go core with React WebUI route was selected because it keeps filesystem rules and operation journaling in one reliable service while giving users both terminal and browser workflows. Embedding the frontend keeps the runtime a single executable. Loopback binding, Host and Origin checks, and a session token constrain the HTTP bridge to the active local operator. Wails remains a separate packaging choice rather than a prerequisite for the browser UI.

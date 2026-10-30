# @allenmuu/agent-manager

Run the Agent Manager Go CLI without installing Go:

```sh
npx @allenmuu/agent-manager --help
npx @allenmuu/agent-manager init --yes
npx @allenmuu/agent-manager web --project .
```

For persistent installation:

```sh
npm install -g @allenmuu/agent-manager
agent-manager --help
```

Requires Node.js >=22, npm, system tar (Windows 10+ includes bsdtar), and initial
network access to npm/GitHub Releases. Supports macOS, Linux and Windows x64 and
arm64. Installation downloads the exact package version's prebuilt executable
and verifies it against the release SHA-256 manifest. Failures exit nonzero.
If install scripts are disabled, the first command acquires the binary instead.
Subsequent commands use the installed executable without distribution downloads.

Every argument is passed to the Go CLI. `install <skill-id>...` installs selected
Skills into a project; it is not a wrapper command for installing the CLI.
`init` installs the global Operator Skill after the CLI is available.

If the global executable is not on PATH, use npx directly or add the global npm
bin directory: `$(npm prefix -g)/bin` on macOS/Linux, the npm prefix on Windows.

[Documentation and source](https://github.com/AllenMuu/agent-manager)

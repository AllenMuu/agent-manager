# Agent Manager

[English](README.md) · **简体中文**

> Agent Manager 是本地优先的代理资源管理工具：管理 Skills、规范化 SubAgent、跨代理任务工件、共享 Memory 配置和运行时治理记录。
>
> 它提供 CLI 和本地 Web 控制台，但**不会启动 AI 代理**。Claude Code、Codex 和 Pi 当前也没有接入运行时策略执行或共享 Memory 的原生适配器。

## 目录

- [快速开始](#快速开始)
- [构建与开发](#构建与开发)
- [功能概览](#功能概览)
- [配置](#配置)
- [命令速查](#命令速查)
- [本地 Web 控制台](#本地-web-控制台)
- [规范化 SubAgent](#规范化-subagent)
- [跨代理任务与评估](#跨代理任务与评估)
- [共享 Memory](#共享-memory)
- [运行时策略与治理](#运行时策略与治理)
- [Skill 操作、安全与恢复](#skill-操作安全与恢复)
- [项目推荐](#项目推荐)
- [兼容性与路线图](#兼容性与路线图)

## 快速开始

仓库已提供 npm 分发实现；以下 npm 命令需在首次发布 npm 包和 GitHub Release 后使用。发布步骤见[发布指南](docs/npm-release.md)。

需要 Node.js 22 或更高版本、npm 和系统 `tar` 命令（macOS、Linux 和 Windows 10+ 通常自带）。通过 npx 直接运行，无需 Go、克隆仓库或手动配置 PATH：

```sh
npx @allenmuu/agent-manager --help
npx @allenmuu/agent-manager init --yes
npx @allenmuu/agent-manager web --project .
```

也可以长期安装 CLI：

```sh
npm install -g @allenmuu/agent-manager
agent-manager --help
```

npm 包装器会从 GitHub Release 下载与包版本一致的当前平台二进制，并在安装前校验 SHA-256。如果禁用了 npm 安装脚本，首次运行时会下载。初次安装需要访问 npm 和 GitHub Release；安装完成后使用本地 Go 二进制。`install <skill-id>...` 等现有命令全部透传。

如果全局 npm 安装后仍提示 `command not found`，macOS/Linux 请将 `$(npm prefix -g)/bin` 加入 PATH，Windows 则将 npm 前缀目录本身加入 PATH；也可以直接使用 npx。macOS/Linux 的 zsh 可执行：

```sh
export PATH="$(npm prefix -g)/bin:$PATH"
# 再执行一次，让新开的终端也能找到命令：
echo 'export PATH="$(npm prefix -g)/bin:$PATH"' >> "$HOME/.zshrc"
```

### 从源码安装

首次 npm 发布前，或需要从源码构建时，使用 Go 1.24 或更高版本及 Git：

```sh
git clone https://github.com/AllenMuu/agent-manager.git
cd agent-manager
mkdir -p "$HOME/.local/bin"
GOBIN="$HOME/.local/bin" go install ./cmd/agent-manager
export PATH="$HOME/.local/bin:$PATH"
agent-manager --help
```

已有本地仓库时，从仓库根目录的 `mkdir -p` 步骤开始。zsh 用户可执行一次以下命令，持久化源码安装的 PATH 配置：

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$HOME/.zshrc"
```

`agent-manager init` 用于安装全局 Operator Skill，前提是 CLI 已经可用；它不会安装 CLI 本身。

CLI 安装完成后，在需要管理的项目目录运行：

```sh
# 安装或更新全局 Operator Skill
agent-manager init --yes

# 查看本机代理适配器
agent-manager agents --project . --json

# 搜索和推荐 Skills
agent-manager search "go"
agent-manager recommend --project .

# 交互式激活，或直接指定 Skill 和目标代理
agent-manager select --project .
agent-manager add <skill> --project . --target codex --yes

# 一次安装多个明确选择的 Skill，或打开专用安装界面
agent-manager install go-helper review --project . --target codex --yes
agent-manager webui --project .

# 启动只监听本机的 Web 控制台
agent-manager web --project . --port 0
```

`skill-manager` 仍是兼容命令，会提示迁移到 `agent-manager`。新脚本和自动化任务请使用 `agent-manager`。

## 构建与开发

从仓库根目录运行 Go 检查和 OpenSpec 命令：

`go build ./cmd/agent-manager` 只会在仓库根目录生成 `./agent-manager`，可用 `./agent-manager --help` 运行。若要在任意目录直接使用 `agent-manager`，请先按“快速开始”安装 CLI 并配置 PATH。

```sh
go build ./cmd/agent-manager
go test ./...
go vet ./...
openspec validate add-local-web-console --strict
openspec doctor
```

Web 控制台的构建产物已嵌入 `internal/webconsole/ui`，运行 Go 二进制不需要 Node.js 或网络。修改 React 控制台时，在 `web/` 目录使用仓库锁定的依赖：

```sh
cd web
npm ci
npm test
npm run build
```

`npm run build` 同时构建两个嵌入式客户端：`web/index.html` 对应 `internal/webconsole/ui`，`web/install/index.html` 对应 `internal/webui/dist`。

前端工具链支持 Node.js 22.12 及更高的 22.x 版本、24.x 或 26 及更高版本。

## 功能概览

| 领域 | 能力 |
|---|---|
| **Skills** | 从本地 Skill 库搜索和推荐 Skills；在 Claude Code、Codex、Pi 项目目录建立机器本地符号链接；支持添加、移除、收编、分叉、诊断、修复和撤销。 |
| **SubAgent** | 以 Agent Manager 下的版本化 YAML 作为规范来源；支持检查和验证，通过适配器安装到受支持的代理。Pi 当前没有已验证的原生 SubAgent 格式。 |
| **跨代理工作流** | 用版本化任务工件组织 intent、spec、plan、implementation、verification 和 lessons；提供角色契约和上下文打包。由外部代理或人工完成任务，Agent Manager 不会启动代理。 |
| **共享 Memory** | 可配置现有的本地文件型提供器；状态检查只读，写入必须显式调用 `memory promote` 并确认。当前没有代理原生 Memory 集成。 |
| **运行时治理** | 校验 AgentPolicy、检查本地 AgentRun、审批和审计事件，并用确定性规则评估治理事件。当前代理适配器不执行策略，也不提供运行时控制。 |
| **评估** | 使用确定性本地规则评估外部代理生成的响应文件或治理事件，不启动代理，也不依赖 LLM 裁判。 |
| **Web 控制台** | 通过带会话令牌的本地网页界面查看目录、诊断和操作计划；写操作仍需单独确认。 |

Skill 激活是机器本地状态，不会生成可提交的项目清单，也不会自动同步到其他机器。`init` 是唯一会修改代理全局 Skill 目录的命令；项目命令只处理项目范围。

## 配置

### Skill 库

默认 Skill 库为 `~/.agents/skills`，可通过 `--config <path>` 指定 YAML 配置文件：

```yaml
library: ~/.agents/skills
```

### 共享 Memory 提供器

当前支持本地文件型 Memory 提供器。配置只保存已有文件的引用；Agent Manager 不会创建或写入该文件来完成状态发现。

```yaml
memory:
  version: v1
  id: local-memory
  provider: file
  configuration:
    kind: file
    name: /Users/me/.local/share/agent-manager/memory.json
  scopes: [user, project]
  capabilities: [read, search]
```

引用路径必须指向一个已存在的绝对路径、普通本地文件，并且不能是符号链接。请在配置中填写非敏感的提供器引用，不要放入凭据。默认不会启用网络提供器，也不会自动启动或配置提供器。

## 命令速查

### Skill 与代理

| 命令 | 用途 |
|---|---|
| `agents` | 查看注册的代理适配器及本机发现结果。 |
| `init` | 验证 CLI，并安装或更新全局 Operator Skill。 |
| `search <query>` | 搜索 Skill 标识符、描述、正文和标签。 |
| `recommend` | 静态分析项目技术栈并推荐匹配的 Skills。 |
| `select` | 交互式搜索、选择 Skills 和目标代理，并确认计划。 |
| `add <skill>` | 激活 Skill 到指定目标代理。 |
| `install <skill-id>...` | 预览并一次安装明确选择的多个 Skills。 |
| `webui` | 启动只监听本机的项目安装界面。 |
| `list` | 查看项目 Skills 的受管理状态。 |
| `remove <skill>` | 移除一个受管理的项目链接。 |
| `adopt <skill>` | 将合适的项目 Skill 收入 Skill 库并建立链接。 |
| `fork <skill>` | 将受管理链接转换成独立的项目副本。 |
| `doctor` | 检查 Skill 库、项目链接和 Git 跟踪状态。 |
| `reconcile` | 修复因 Skill 库移动而失效的受管理链接。 |
| `undo` | 撤销最近一次受日志管理的项目操作。 |
| `delete <skill>` | 删除 Skill 库中的 Skill；需要 `--force` 和确认。 |

### SubAgent 与 Memory

| 命令 | 用途 |
|---|---|
| `subagents list/show/validate` | 查看或验证规范化 SubAgent 定义；支持 JSON 输出。 |
| `subagents install <id>` | 通过适配器安装 SubAgent，操作受保护、需确认并记录日志。 |
| `subagents remove <id>` | 移除受 Agent Manager 管理的 SubAgent 表示。 |
| `memory status` | 检查 Memory 提供器状态和各代理的能力映射。 |
| `memory promote` | 明确确认后，将文本或 lessons 工件追加到配置的 Memory 提供器。 |

### 跨代理工作流与治理

| 命令 | 用途 |
|---|---|
| `task init` | 创建任务和初始 intent 工件。 |
| `artifacts list/show/validate/render` | 查看、校验和渲染任务工件。 |
| `artifacts save` | 校验并保存工件到任务；需要单独确认。 |
| `roles` / `roles bind` | 查看角色契约，或检查契约与代理适配器的能力匹配。 |
| `roles context <task-id>` | 为外部代理组装指定角色的 JSON 上下文。 |
| `eval list/run/compare` | 查看评估套件、评估候选响应或治理事件，并比较结果。 |
| `policies validate/list/show` | 校验和查看本地版本化 AgentPolicy。 |
| `runs list/show/events/kill` | 检查本地 AgentRun、策略快照、审批及审计事件；按能力请求终止。 |
| `approvals list/show/approve/reject/expire` | 查看审批请求并记录人工决策。 |
| `web` | 启动本地 Web 控制台。 |

## 本地 Web 控制台

启动一个只监听 IPv4 回环地址 `127.0.0.1` 的控制台：

```sh
agent-manager web --project . --port 0
```

`--project` 可省略，之后可在控制台注册项目；`--port 0` 会让系统选择可用端口。启动命令会打印入口地址，其中 URL 片段带有随机的进程级会话令牌。浏览器会将令牌存入当前标签页的 `sessionStorage` 并从地址栏移除；API 使用 Bearer 令牌，写请求校验来源，不开放 CORS。按 `Ctrl+C` 会关闭会话并丢弃待处理计划。

每个进程只注册一个规范项目。控制台没有任意路径读取接口；激活、移除和可用的撤销操作会显示由服务端生成、有效期为 10 分钟的计划，并要求操作者再次确认。替换冲突还需要额外的强制确认。撤销仅覆盖控制台管理的项目目录；涉及共享 Skill 库或其他非托管路径的操作不会提供撤销入口。

## 规范化 SubAgent

规范化定义以版本化、与代理无关的 YAML 保存在 `<agent-manager-root>/subagents`；默认根目录是 `~/.agents`，可用 `--root <path>` 指定其他位置。Skill 引用会根据配置的 Skill 库进行校验。`list`、`show` 和 `validate` 只检查规范定义，不会生成或修改代理原生文件。

安装时，Agent Manager 根据所选适配器渲染规范定义。Claude Code 和 Codex 有已验证的文件表示；Pi 会明确报告不支持，不会写入猜测的格式。`subagents list --json` 保留定义和诊断信息；`subagents validate --json` 返回验证状态及诊断。

## 跨代理任务与评估

任务工件存放在项目的 `.agents/tasks/<task-id>/`，常见流程为：

```text
intent → spec → plan → implementation → verification
                                  ↘ lessons
```

工件使用版本化 `v1` 信封，类型包括 `intent`、`spec`、`plan`、`implementation`、`verification` 和 `lessons`。Lessons 使用带范围和置信度的结构化条目，不会自动写入 Memory。

内置角色包括 planner、implementer、reviewer 和 verifier。角色契约声明输入、输出及文件系统、Shell、网络权限；`roles bind` 会报告适配器无法保证的能力。`roles context` 会将受限的项目指引、已选 Skills、角色输入工件和可选 Memory 搜索结果打包为交接 JSON，但不会调用代理或写入 Memory。后续步骤由外部代理或人工完成，再通过 `artifacts save` 保存结果。

本地评估套件位于 `evals/<suite>/cases/<case-id>/`，结果保存在被忽略的 `.agent-manager/evals/`。响应文件评估读取外部生成的 `<case-id>.md`，治理套件读取本地事件证据；两者都使用确定性规则，不会启动代理或调用 LLM 裁判。

## 共享 Memory

`memory status` 检查配置的提供器、能力和作用范围。若配置了本地文件提供器，Agent Manager 会确认文件状态；Claude Code、Codex 和 Pi 当前没有原生共享 Memory 映射，因此会显示为不支持。

写入必须由操作者明确调用并确认。可提交文本或经过校验的 lessons 工件：

```sh
agent-manager memory promote --scope user --knowledge "..." --yes
agent-manager memory promote --scope project \
  --lessons .agents/tasks/task-1/lessons.yaml --yes
```

计划只展示提供器、作用范围和字节数，不会回显知识内容。`memory promote` 写入的是提供器拥有的数据，不属于文件操作日志，也不能通过 `undo` 撤销。Skill、SubAgent、状态、目录或列表流程都不会自动复制内容或代理会话数据到 Memory。

## 运行时策略与治理

`AgentPolicy v1` 可声明工具允许/拒绝规则、网络域名、凭据作用范围、SubAgent 并发与总量限制、预算、审批要求和终止条件。策略使用严格 YAML 校验；未知字段、重复规则、不支持的版本和非规范主机名都会被拒绝。用以下命令校验策略文件：

```sh
agent-manager policies validate ./policy.yaml
```

供 `policies list` 和 `policies show` 使用的用户级策略位于 `os.UserConfigDir()/agent-manager/policies/`。共享的 AgentRun 状态位于 `os.UserConfigDir()/agent-manager/runs/state.json`，每个运行记录保存不可变的有效策略快照、运行时标签、状态和能力警告。

当前目录适配器没有运行时控制接口，因此策略执行和终止可能报告能力不支持。CLI 没有 `runs start` 命令，也不会启动代理。`approvals approve/reject/expire` 只记录人工决定和关联审计事件，不会执行操作或恢复代理运行。审计记录包含安全的操作标签、决策和稳定原因码，不复制原始事件元数据、凭据作用范围、决策详情或不可信的操作者文本。更多细节见[运行时策略与治理说明](docs/runtime-policy-governance.md)。

## Skill 操作、安全与恢复

Skill 文件系统变更会先生成计划，并通过 `--yes` 或交互确认后执行。成功的 Skill 变更会写入可逆操作日志；`artifacts save` 和 `memory promote` 使用各自的确认流程，不属于该文件系统日志。

### 冲突处理

目标位置存在未受管理内容时，`add` 默认拒绝覆盖。只有明确选择替换策略并提供强制确认后才能继续；操作本身仍需 `--yes`：

```sh
agent-manager add <skill> --project . --target codex \
  --conflict replace --force --yes
```

### 兼容性声明

Skill 可在 `.skill-manager.yaml` 中声明目标代理兼容性：

```yaml
compatibility:
  - codex
```

若目标代理未在声明中列出，计划会显示警告，但明确确认后仍可安装。

### Git 忽略与恢复

托管链接是机器本地状态。`doctor` 会显示链接的 Git 跟踪状态；需要忽略时，可让它在 `.gitignore` 中追加精确路径：

```sh
agent-manager doctor --project .
agent-manager doctor --project . --update-gitignore --yes
agent-manager reconcile --project . --yes
agent-manager undo --project . --yes
```

`doctor --update-gitignore` 不会重写其他规则。`reconcile` 使用操作日志修复 Skill 库移动后失效的链接；`undo` 恢复最近一次项目操作。默认情况下，未受管理的目录、普通文件和意外链接都会保持原样。

## 项目推荐

`recommend` 静态读取项目标记和 Skill 库，不执行项目代码、不联网，也不修改文件：

```sh
agent-manager recommend
agent-manager recommend --project ../service
agent-manager recommend --project . --json
```

当前标记包括 `go.mod`、`package.json`、`tsconfig.json`、`pom.xml`、`build.gradle`、`build.gradle.kts`、`pyproject.toml`、`Cargo.toml`、`Gemfile`、`Dockerfile`、Compose 文件，以及 `.claude`、`.codex` 和 `.agents` 目录。技术词汇覆盖 Go、Node.js、TypeScript、Maven、Gradle、Python、Rust、Ruby、Docker、Compose、Express、NestJS、React、Next.js、Spring Boot、Django、FastAPI、Rails、PostgreSQL、MySQL、Redis、MongoDB 和文档定义的代理标记。

推荐按精确技术标签、标识符与描述、正文的顺序匹配，并说明命中原因。扫描会跳过依赖和构建目录及目录符号链接；每个标记文件最多读取 1 MiB，最多遍历 10,000 个条目。Monorepo 中每个带有独立标记的子项目会单独分析。证据不足时返回 `insufficient_evidence`；没有匹配 Skill 时返回 `no_catalog_match`。

## 兼容性与路线图

`skill-manager` 是现有 Skill 工作流的兼容别名，与 `agent-manager` 共用实现并显示迁移提示。版本化配置、伴随元数据、项目链接和旧版操作日志会被读取并在内存中规范化，不会自动重写。新的 SubAgent 和 Memory 工作流不保证与旧脚本参数兼容。

Agent Manager 的本地任务工件和评估能力不会生成自主代理、同步完整对话、获取远程数据或成为分布式工作流引擎。

- **自主委派**：后续可使用版本化任务工件和角色契约，而无需绑定特定代理的任务状态格式。
- **React WebUI**：CLI 之后的本地 Web 界面。
- **Wails**：为 WebUI 提供桌面应用封装。


## 结构化 Memory

显式选择 `structured-local` 后，可注册稳定项目身份、确认新增/更新/替代/遗忘/导入，
并通过同一 Gateway 检索有来源且受结果数和字节预算约束的知识。`roles context`
只读使用相同路径，不隐式写入。详见 [Memory Gateway 使用说明](docs/memory-gateway.md)。
旧 `file` provider 和显式 `memory promote` 保持兼容；状态分别报告请求、实际实现与可用性。
Memory 生命周期不进入可撤销的 Skill 文件系统 operation journal。

另见 [离线运行时权限与保护检查](docs/runtime-enforcement-preflight.md)：R1 新增声明格式、分维度能力和执行前检查，不启动或证明真实运行时保护。

另见 [可恢复运行时生命周期](docs/runtime-enforcement-lifecycle.md)：R2 新增中立 provider/coordinator 边界、持久化操作恢复和离线 mock 证据，不启动真实模型，也不建立沙箱保护。

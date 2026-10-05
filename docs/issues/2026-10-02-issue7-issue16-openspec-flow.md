# Issue #7 / #16 OpenSpec 交付映射

日期：2026-10-02（Asia/Shanghai）

已批准的路线按 ticket 分成独立 change。此前已发布 tickets，但缺少这 10 项正式 OpenSpec 规划；本次补齐，不重复发布、不将规划就绪记为实现完成。本文只保存映射和门槛；实现 checklist 的唯一来源是各 change 的 tasks.md。

## 当前范围与依赖

| 切片 | GitHub ticket | OpenSpec change | 真正阻塞项 | 当前状态 |
| --- | --- | --- | --- | --- |
| F0 | [#18](https://github.com/AllenMuu/agent-manager/issues/18) | [fix-approved-invocation-retry-lineage](../../openspec/changes/archive/2026-10-02-fix-approved-invocation-retry-lineage/proposal.md) | 无 | 实现/审查 7/7；[PR #30](https://github.com/AllenMuu/agent-manager/pull/30) 已合并；#18 已关闭，规格同步/归档已在本分支完成 |
| M1 | [#19](https://github.com/AllenMuu/agent-manager/issues/19) | [define-scoped-memory-contracts](../../openspec/changes/archive/2026-10-02-define-scoped-memory-contracts/proposal.md) | 无 | 实现/审查 10/10；PR #32 已合并、#19 已关闭；本分支已同步/归档 |
| M2 | [#20](https://github.com/AllenMuu/agent-manager/issues/20) | [add-structured-local-memory-store](../../openspec/changes/archive/2026-10-02-add-structured-local-memory-store/proposal.md) | #19 | 实现验收 9/9；PR #34 已合并主分支 `b388a67`，#20 CLOSED；同步/归档已随 #21 / PR #36 交付 |
| M3 | [#21](https://github.com/AllenMuu/agent-manager/issues/21) | [add-memory-gateway-and-cli](../../openspec/changes/archive/2026-10-03-add-memory-gateway-and-cli/proposal.md) | #20 | 实现验收 9/9；PR #36 已合并 `ae1745a`，#21 CLOSED；5 requirements / 7 scenarios 已同步归档，归档文档已随 PR #38 进入主分支 |
| M4 | [#22](https://github.com/AllenMuu/agent-manager/issues/22) | [add-mem0-memory-provider](../../openspec/changes/add-mem0-memory-provider/proposal.md) | #19 | 实现验收 10/10；5 requirements / 7 scenarios 有证据；实际 Go Mem0 smoke 与离线验证分开；PR #38 已合并主分支 `863ebc2`，#22 CLOSED；M4 同步/归档文档仅在待验收的 M5 分支，尚未进入主分支 |
| M5 | [#23](https://github.com/AllenMuu/agent-manager/issues/23) | [share-memory-across-codex-and-claude](../../openspec/changes/share-memory-across-codex-and-claude/proposal.md) | #21, #22 | 另一本地分支 `feature/issue23-cross-agent-memory` 已保存部分实现 `a04959e`，2/9 tasks；离线检查 PASS；真实 Claude 当前服务订阅阻塞，用户选择恢复原服务后验收；无 PR/合并，#23 OPEN；本 R1 分支不含 M5 源码 |
| R1 | [#24](https://github.com/AllenMuu/agent-manager/issues/24) | [define-runtime-enforcement-capabilities](../../openspec/changes/define-runtime-enforcement-capabilities/proposal.md) | 无 | 本分支实现/审查 10/10；5 requirements / 6 scenarios；规格、质量及最终只读 OCR PASS（13/13）；源码 `931f57a`；新增元数据/PR 正文增量审查及发布/合并待完成，未关闭 #24 |
| R2 | [#25](https://github.com/AllenMuu/agent-manager/issues/25) | [add-runtime-enforcement-lifecycle](../../openspec/changes/add-runtime-enforcement-lifecycle/proposal.md) | #24, #18 | proposal/design/spec/tasks 已生成；实现 0 项 |
| R3 | [#26](https://github.com/AllenMuu/agent-manager/issues/26) | [add-runtime-permission-proposals](../../openspec/changes/add-runtime-permission-proposals/proposal.md) | #25 | proposal/design/spec/tasks 已生成；实现 0 项 |
| R4 | [#27](https://github.com/AllenMuu/agent-manager/issues/27) | [apply-confirmed-policy-revisions](../../openspec/changes/apply-confirmed-policy-revisions/proposal.md) | #26 | proposal/design/spec/tasks 已生成；实现 0 项 |
| R5 | [#28](https://github.com/AllenMuu/agent-manager/issues/28) | [integrate-experimental-openshell](../../openspec/changes/integrate-experimental-openshell/proposal.md) | #27 | proposal/design/spec/tasks 已生成；实现 0 项 |

M1 与 R1 是不依赖其他 tickets 的 frontier；F0 优先修复现有基线，但不是 M1 的技术阻塞。M4 可在 M1 后开发，其最终双 Agent 演示仍由 M5 汇合 M3/M4。R2 必须等待 R1 与 F0 的交付；后续权限提案、确认修订和真实 OpenShell 按 R3→R4→R5 顺序。GitHub 的 ready-for-agent 标签本身不证明依赖已经满足。

## Ticket 验收到任务与场景

每个新 ticket 的验收项按原顺序编号 1–5，对应同一行的 requirement 和 tasks.md 中 2.N；实现前先完成 1.1 的依赖/公共测试边界确认。以下证据是计划的验证方式，不是已通过测试的声明。

| Ticket / 验收项 | Task | Requirement | Scenario / 计划验证 |
| --- | --- | --- | --- |
| #19 / 1 | 2.1 | [Explicit owner partitions](../../openspec/changes/archive/2026-10-02-define-scoped-memory-contracts/specs/scoped-memory-contracts/spec.md) | Missing ownership identifiers；Two-project isolation |
| #19 / 2 | 2.2 | [Canonical typed record round trip](../../openspec/changes/archive/2026-10-02-define-scoped-memory-contracts/specs/scoped-memory-contracts/spec.md) | Remember and recall project knowledge |
| #19 / 3 | 2.3 | [Honest optional capabilities and errors](../../openspec/changes/archive/2026-10-02-define-scoped-memory-contracts/specs/scoped-memory-contracts/spec.md) | Unsupported recall versus unavailable storage；Canceled operation |
| #19 / 4 | 2.4 | [Deterministic neutral contract implementation](../../openspec/changes/archive/2026-10-02-define-scoped-memory-contracts/specs/scoped-memory-contracts/spec.md) | Portable contract suite |
| #19 / 5 | 2.5 | [Text and Skill compatibility](../../openspec/changes/archive/2026-10-02-define-scoped-memory-contracts/specs/scoped-memory-contracts/spec.md) | Legacy promotion remains available |
| #20 / 1 | 2.1 | [Durable owner and provenance](../../openspec/changes/archive/2026-10-02-add-structured-local-memory-store/specs/structured-local-memory-store/spec.md) | Reopen owned record |
| #20 / 2 | 2.2 | [Conditional serialized updates](../../openspec/changes/archive/2026-10-02-add-structured-local-memory-store/specs/structured-local-memory-store/spec.md) | Competing version updates |
| #20 / 3 | 2.3 | [Atomic lifecycle and history](../../openspec/changes/archive/2026-10-02-add-structured-local-memory-store/specs/structured-local-memory-store/spec.md) | Supersede and forget；Supersession fails before commit |
| #20 / 4 | 2.4 | [Recoverable local writes](../../openspec/changes/archive/2026-10-02-add-structured-local-memory-store/specs/structured-local-memory-store/spec.md) | Failure and retry evidence |
| #20 / 5 | 2.5 | [Explicit legacy import](../../openspec/changes/archive/2026-10-02-add-structured-local-memory-store/specs/structured-local-memory-store/spec.md) | No implicit conversion；Confirmed import |
| #21 / 1 | 2.1 | [Truthful provider discovery](../../openspec/changes/archive/2026-10-03-add-memory-gateway-and-cli/specs/memory-gateway-and-cli/spec.md) | Configured search is not implemented |
| #21 / 2 | 2.2 | [Confirmed owned mutations](../../openspec/changes/archive/2026-10-03-add-memory-gateway-and-cli/specs/memory-gateway-and-cli/spec.md) | Mutation is not confirmed；Stable project identity |
| #21 / 3 | 2.3 | [Bounded deterministic retrieval](../../openspec/changes/archive/2026-10-03-add-memory-gateway-and-cli/specs/memory-gateway-and-cli/spec.md) | Bounded attributed search；Wrong-owner query |
| #21 / 4 | 2.4 | [Shared read-only task-context path](../../openspec/changes/archive/2026-10-03-add-memory-gateway-and-cli/specs/memory-gateway-and-cli/spec.md) | Context handoff reads knowledge |
| #21 / 5 | 2.5 | [Independent failure and recovery semantics](../../openspec/changes/archive/2026-10-03-add-memory-gateway-and-cli/specs/memory-gateway-and-cli/spec.md) | Provider outage |
| #22 / 1 | 2.1 | [Canonical remote operation mapping](../../openspec/changes/add-mem0-memory-provider/specs/mem0-memory-provider/spec.md) | Remote canonical round trip |
| #22 / 2 | 2.2 | [Honest remote semantic capabilities](../../openspec/changes/add-mem0-memory-provider/specs/mem0-memory-provider/spec.md) | Backend cannot atomically supersede |
| #22 / 3 | 2.3 | [Bounded transport and uncertain writes](../../openspec/changes/add-mem0-memory-provider/specs/mem0-memory-provider/spec.md) | Write response times out；Authentication failure |
| #22 / 4 | 2.4 | [Opaque secret references](../../openspec/changes/add-mem0-memory-provider/specs/mem0-memory-provider/spec.md) | Authenticated request diagnostics |
| #22 / 5 | 2.5 | [Offline contract and opt-in live evidence](../../openspec/changes/add-mem0-memory-provider/specs/mem0-memory-provider/spec.md) | Ordinary offline test run；Explicit live smoke |
| #23 / 1 | 2.1 | [Two real agent readers and writers](../../openspec/changes/share-memory-across-codex-and-claude/specs/cross-agent-shared-memory/spec.md) | Codex writes and Claude reads；Real Mem0 two-agent acceptance |
| #23 / 2 | 2.2 | [Effective version convergence](../../openspec/changes/share-memory-across-codex-and-claude/specs/cross-agent-shared-memory/spec.md) | Both agents observe replacement |
| #23 / 3 | 2.3 | [Owner isolation and inspectable provenance](../../openspec/changes/share-memory-across-codex-and-claude/specs/cross-agent-shared-memory/spec.md) | Cross-project read attempt |
| #23 / 4 | 2.4 | [Per-agent tested capability status](../../openspec/changes/share-memory-across-codex-and-claude/specs/cross-agent-shared-memory/spec.md) | One integration lacks write support |
| #23 / 5 | 2.5 | [One canonical source of truth](../../openspec/changes/share-memory-across-codex-and-claude/specs/cross-agent-shared-memory/spec.md) | Integration is enabled |
| #24 / 1 | 2.1 | [Separate effective permissions and controls](../../openspec/changes/define-runtime-enforcement-capabilities/specs/runtime-enforcement-preflight/spec.md) | Inspect indirect permissions |
| #24 / 2 | 2.2 | [Fail-closed mandatory preflight](../../openspec/changes/define-runtime-enforcement-capabilities/specs/runtime-enforcement-preflight/spec.md) | Required credential mediation is missing；Optional gap |
| #24 / 3 | 2.3 | [Dimension-specific policy update support](../../openspec/changes/define-runtime-enforcement-capabilities/specs/runtime-enforcement-preflight/spec.md) | Static filesystem and dynamic network |
| #24 / 4 | 2.4 | [Truthful noop and placement boundaries](../../openspec/changes/define-runtime-enforcement-capabilities/specs/runtime-enforcement-preflight/spec.md) | Noop or directory adapter is selected |
| #24 / 5 | 2.5 | [Deterministic non-executing preflight](../../openspec/changes/define-runtime-enforcement-capabilities/specs/runtime-enforcement-preflight/spec.md) | Local preflight suite |
| #25 / 1 | 2.1 | [Persisted neutral external lineage](../../openspec/changes/add-runtime-enforcement-lifecycle/specs/runtime-enforcement-lifecycle/spec.md) | Inspect a prepared mock run |
| #25 / 2 | 2.2 | [Recoverable preparation and start](../../openspec/changes/add-runtime-enforcement-lifecycle/specs/runtime-enforcement-lifecycle/spec.md) | External prepare succeeds but local confirmation fails；Restart after uncertain start |
| #25 / 3 | 2.3 | [Confirmed lifecycle transitions](../../openspec/changes/add-runtime-enforcement-lifecycle/specs/runtime-enforcement-lifecycle/spec.md) | Termination is not acknowledged |
| #25 / 4 | 2.4 | [Honest optional lifecycle functions](../../openspec/changes/add-runtime-enforcement-lifecycle/specs/runtime-enforcement-lifecycle/spec.md) | Provider cannot pause |
| #25 / 5 | 2.5 | [Deterministic lifecycle evidence](../../openspec/changes/add-runtime-enforcement-lifecycle/specs/runtime-enforcement-lifecycle/spec.md) | Mock lifecycle and invocation |
| #26 / 1 | 2.1 | [Denial-bound expiring proposals](../../openspec/changes/add-runtime-permission-proposals/specs/runtime-permission-proposals/spec.md) | Denied network action requests permission |
| #26 / 2 | 2.2 | [Trusted human decision authority](../../openspec/changes/add-runtime-permission-proposals/specs/runtime-permission-proposals/spec.md) | Agent spoofs human metadata；Authorized operator decides |
| #26 / 3 | 2.3 | [Expiry and base revision checks](../../openspec/changes/add-runtime-permission-proposals/specs/runtime-permission-proposals/spec.md) | Stale or expired request |
| #26 / 4 | 2.4 | [Delegation ceiling](../../openspec/changes/add-runtime-permission-proposals/specs/runtime-permission-proposals/spec.md) | Permission exceeds delegation |
| #26 / 5 | 2.5 | [Distinct non-mutating proposal decisions](../../openspec/changes/add-runtime-permission-proposals/specs/runtime-permission-proposals/spec.md) | Proposal is rejected or merely approved |
| #27 / 1 | 2.1 | [Immutable historical policy references](../../openspec/changes/apply-confirmed-policy-revisions/specs/confirmed-policy-revisions/spec.md) | Change policy after prior audit |
| #27 / 2 | 2.2 | [Desired versus confirmed applied revision](../../openspec/changes/apply-confirmed-policy-revisions/specs/confirmed-policy-revisions/spec.md) | Provider only accepts update；Exact application is confirmed |
| #27 / 3 | 2.3 | [Fail-closed application recovery](../../openspec/changes/apply-confirmed-policy-revisions/specs/confirmed-policy-revisions/spec.md) | Wrong acknowledgement or timeout |
| #27 / 4 | 2.4 | [Reconstructable permission change lineage](../../openspec/changes/apply-confirmed-policy-revisions/specs/confirmed-policy-revisions/spec.md) | Inspect approved permission change |
| #27 / 5 | 2.5 | [Idempotent and source-aware events](../../openspec/changes/apply-confirmed-policy-revisions/specs/confirmed-policy-revisions/spec.md) | Duplicate stale event |
| #28 / 1 | 2.1 | [Opt-in neutral OpenShell adapter](../../openspec/changes/integrate-experimental-openshell/specs/experimental-openshell-integration/spec.md) | Adapter is not selected |
| #28 / 2 | 2.2 | [Tested enforcement and credential diagnostics](../../openspec/changes/integrate-experimental-openshell/specs/experimental-openshell-integration/spec.md) | Inspect weaker credential isolation |
| #28 / 3 | 2.3 | [Static policy change handling](../../openspec/changes/integrate-experimental-openshell/specs/experimental-openshell-integration/spec.md) | Filesystem change after start |
| #28 / 4 | 2.4 | [Real sandbox approval smoke evidence](../../openspec/changes/integrate-experimental-openshell/specs/experimental-openshell-integration/spec.md) | Real denied network retry |
| #28 / 5 | 2.5 | [No implicit external setup](../../openspec/changes/integrate-experimental-openshell/specs/experimental-openshell-integration/spec.md) | Offline workflows |

F0 的逐场景验证与已执行结果保存在 [delivery.md](../../openspec/changes/archive/2026-10-02-fix-approved-invocation-retry-lineage/delivery.md)，不复制其实现清单。

## 必须经过的流程

1. **Propose**：使用 spec-driven schema，按 proposal→specs/design→tasks 的 artifact 依赖生成；`openspec status --change <name> --json` 检查完整规划，`openspec validate <name> --strict` 检查结构。
2. **Apply + TDD**：核对所选 ticket 的原生阻塞关系与已交付行为，读取 `openspec instructions apply --change <name> --json` 返回的所有上下文；只执行当前切片。确认公共测试边界后逐条 red→green，随后才勾选 tasks。CLI 的 apply ready 只说明产物具备，不代表 tickets 依赖、测试边界或发布门槛已经满足。
3. **Verify**：逐 requirement/scenario 对照实现与测试/真实集成证据；项目未安装可选 `openspec-verify-change` skill，使用等价人工证据核对，并与 CLI validate 区分。运行全量 Go 测试、相关 race、vet、CLI build、strict validate、doctor 与 diff checks。
4. **Review / PR**：普通独立审查及修复验证后，执行指定 gpt-6.1-sol/high 的只读 OCR 审查；完整范围无可行动问题才发布。任何可行动发现、指定 reviewer 不可用或重要覆盖缺口都保留未完成门槛，不自动换模型、修复或发布。
5. **Sync / Archive**：当前切片验收且代码进入目标分支、合并授权满足后同步增量规格并归档；单个子 ticket 不自动关闭父 issue。只有规划完整或 PR 已创建均不等于可以归档。

## 兼容与后续边界

Memory 保留已有文本 promotion，新增结构化存储显式选择，Mem0 为首个真实网络引擎；不自动导入旧文本、复制 Agent 私有 Memory、执行 SKILL/TASK 内容或安装服务。执行治理保持 Agent Manager 控制面与外部沙箱执行面分离，mock 通过不构成真实隔离证据。

本轮 V1 使用 USER/PROJECT/AGENT/SESSION，GLOBAL、自动抽取/归并、TencentDB/Graphiti 后续 provider 延后；运行治理的 Native Sandbox、watchdog、checkpoint 与形式化证明延后。父 issue 仍保持开放，其完成需对照完整已批准范围和原始验收，不能因某一个切片完成而关闭。关闭 #7 还必须有真实 Mem0 + Codex/Claude 的联合演示证据（版本/环境、更新后两者看到同一有效记录、来源）；不能用 Mem0 fixtures 与本地双 Agent 演示拼接代替。合并、sync、archive 是 implementation tasks 完成后的外部生命周期步骤，不放入 apply 的实现 checklist。

## 本次规划证据

10 个 change 共 50 条 requirement、63 个 scenario；每项已有 schema 要求的全部产物，apply state 为 ready，所有新增实现 tasks 未勾选。逐项 strict validate、当前 root doctor 和 ticket 原生依赖回读结果见下方验证记录；结构校验不宣称实现已验收。

## 验证记录

- `openspec validate --changes --strict --json`：19/19 个现有与新增 change 通过（含最新 main 的一键安装 change）。
- 每个新增 change 的 `status --json`：规划产物完整；`instructions apply --json`：ready，已实施项为 0。10 项共 94 个待执行实现 tasks。
- `openspec doctor`：当前 root 健康；无跨 store references。doctor 不验证 GitHub ticket 依赖。
- GitHub 原生 blocked_by 回读：#19–#28 的全部依赖与已批准拆分一致；新增 change 内未虚构 CLI 会自动强制这些依赖。
- 本地检查：50 条 requirement、63 个 scenario；映射链接全部有效，新增产物无占位符、尾随空白或虚假完成勾选。
- 普通独立规划审查发现 M5 缺少真实 Mem0 双 Agent 联合场景，现已补入 spec、task 2.1 和父级验收门槛；普通独立最终复核已通过，无剩余可行动规划问题。
- #18 实现的全量 Go/race/vet/build 已通过；最终 gpt-6.1-sol/high OCR 首次因额度限制未完成，明确重试已通过，纳入 2/2、补充审查排除项 8/8、完整覆盖 10/10；[PR #30](https://github.com/AllenMuu/agent-manager/pull/30) 已合并（`2d2963ce`），#18 已关闭；此分支同步并归档其已验收规格。

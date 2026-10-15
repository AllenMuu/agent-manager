import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import { APIError } from './api'
import type { MessageRef } from './types'

export type Locale = 'en' | 'zh-CN'
type Values = Record<string, string | number>
type LanguageContextValue = {
  locale: Locale
  setLocale: (locale: Locale) => void
  t: (source: string, values?: Values) => string
  message: (ref: MessageRef | undefined, fallback: string) => string
  installationMessage: (source: string) => string
  error: (error: unknown) => string
}

const preferenceKey = 'agent-manager.console-language'
const previousInstallPreferenceKey = 'agent-manager.webui.language'

const zh: Record<string, string> = {
  'Agent Manager': 'Agent Manager',
  'LOCAL CONSOLE': '本地控制台',
  'LOCAL SESSION': '本地会话',
  'Open the console entry link': '请打开控制台入口链接',
  'The session key is kept in this browser tab. Start <code>agent-manager web</code> and open the URL it prints.': '会话密钥保存在当前浏览器标签页中。请运行 <code>agent-manager web</code>，然后打开终端输出的网址。',
  'The session key is kept in this browser tab. Start ': '会话密钥保存在当前浏览器标签页中。请运行 ',
  ' and open the URL it prints.': '，然后打开终端输出的网址。',
  'Main navigation': '主导航',
  'PROJECT DESK': '项目工作台',
  Workspace: '工作区',
  Skills: '技能',
  SubAgents: '子代理',
  Diagnostics: '诊断',
  Operations: '操作记录',
  'Local only': '仅本地',
  'Local': '本地',
  'Session active': '会话已连接',
  'Project changes are reviewed before they are applied.': '项目变更会先经过审查，再予以应用。',
  'No project registered': '尚未注册项目',
  'Choose a local directory to begin': '选择一个本地目录以开始',
  Refresh: '刷新',
  'Dismiss message': '关闭提示',
  'LOCAL SESSION · NO NETWORK CONTENT': '本地会话 · 不加载网络内容',
  'START WITH A PROJECT': '从项目开始',
  'Choose one local workspace.': '选择一个本地工作区。',
  'The console reads this directory locally and only changes project state through reviewed operation plans. It does not expose general file browsing.': '控制台在本地读取此目录，仅通过审查过的操作方案修改项目状态；不提供通用文件浏览功能。',
  'Project directory': '项目目录',
  'Checking…': '检查中…',
  'Inspect project': '检查项目',
  'Paste an absolute path or a path relative to where the console was started.': '请粘贴绝对路径，或相对于控制台启动目录的路径。',
  'SESSION\nBOUND': '会话\n绑定',
  PROJECT: '项目',
  SESSION: '会话',
  BOUND: '绑定',
  'Reading this project…': '正在读取项目…',
  'Workspace is ready': '工作区已就绪',
  'Refresh to read the project inventory.': '刷新以读取项目清单。',
  'PROJECT SNAPSHOT': '项目概览',
  'Read only': '只读',
  '{count} signals found': '发现 {count} 项技术特征',
  'No stack signals yet': '尚未发现技术栈特征',
  'Static markers were read from the project tree.': '已从项目目录中读取静态标记。',
  'The scan reached its traversal limit; results are partial.': '扫描已达到遍历上限，当前结果不完整。',
  'Evidence · {path}': '依据 · {path}',
  'Add a supported project marker to see evidence here.': '添加受支持的项目标记后，相关依据会显示在这里。',
  'Partial scan · review scan diagnostics': '扫描不完整 · 请查看扫描诊断',
  'AGENT ADAPTERS': 'Agent 适配器',
  '{count} adapter needs attention or is unsupported.': '{count} 个适配器需要处理或暂不受支持。',
  '{count} adapters need attention or are unsupported.': '{count} 个适配器需要处理或暂不受支持。',
  'SKILL RECOMMENDATIONS': 'Skill 推荐',
  'Review activation →': '查看启用方案 →',
  'No matching Skills': '没有匹配的 Skill',
  'Search the eligible library to browse available Skills.': '搜索可用库以浏览可用的 Skill。',
  'PROJECT INVENTORY': '项目清单',
  'Plan removal': '查看移除方案',
  'No project Skills': '项目中没有 Skill',
  'This project has no entries in supported Skill locations.': '此项目在受支持的 Skill 位置中没有条目。',
  'DIAGNOSTICS': '诊断',
  '{count} scan notes': '{count} 条扫描记录',
  'Doctor findings and supported repair plans are available on Diagnostics.': '可在“诊断”中查看 Doctor 问题，并为支持的情况预览修复方案。',
  'Open diagnostics': '打开诊断',
  'Recommended Skills': '推荐 Skill',
  'RECOMMENDED SKILLS': '推荐 Skill',
  'Retry recommendations': '重试推荐',
  'Eligible library': '可用库',
  'Skill detail': 'Skill 详情',
  'Read details →': '查看详情 →',
  'Deselect {identifier}': '取消选择 {identifier}',
  '{count} selected': '已选择 {count} 项',
  'Selected Skills': '已选 Skill',
  'Installation targets': '安装目标',
  'Preview installation': '预览安装',
  'Review installation': '审查安装',
  'Confirm installation': '确认安装',
  'Cancel installation': '取消安装',
  'Installation could not be completed.': '无法完成安装。',
  'Could not read the local Skill library.': '无法读取本地技能库。',
  'Could not scan this project.': '无法扫描此项目。',
  'Could not create an installation preview.': '无法创建安装预览。',
  'Could not create an installation preview. Check that the local service is running and retry.': '无法创建安装预览。请确认本地服务仍在运行后重试。',
  'Installation could not be completed. Check that the local service is running and retry.': '无法完成安装。请确认本地服务仍在运行后重试。',
  'Could not reach the local Agent Manager service. Check that it is still running and retry.': '无法连接本地 Agent Manager 服务。请确认服务仍在运行后重试。',
  'Selected Skills installed successfully. The operation is recorded and can be undone with the CLI.': '所选技能已成功安装。此操作已记录，可使用 CLI 撤销。',
  'Replacement preview requested. Review the affected paths before confirming.': '已请求预览替换方案。请在确认前检查受影响的路径。',
  'activate selected skills': '启用所选技能',
  'create absolute link': '创建绝对路径链接',
  'replace conflicting path with absolute link': '用绝对路径链接替换冲突路径',
  'refuse conflicting destination': '拒绝替换冲突目标',
  'replace conflict (force required)': '替换冲突项（需要强制确认）',
  'resource is already installed; no changes made': '资源已安装；未进行更改',
  'invalid installation request': '安装请求无效',
  'no installation changes': '没有需要执行的安装变更',
  'installation preview expired or unavailable': '安装预览已过期或不可用',
  'conflict strategy requires force confirmation': '替换冲突项需要明确的强制确认',
  'refusing unmanaged or unexpected path': '为保护数据，已拒绝操作非托管或异常路径',
  'Skill "{skill}" is not declared compatible with {target}': '技能“{skill}”未声明与 {target} 兼容',
  'The project changed after preview. Review this updated plan, then confirm again.': '预览后项目已发生变化。请审查更新后的方案，再次确认。',
  'Selected Skills installed successfully.': '已成功安装所选 Skill。',
  '{count} project scan note': '{count} 条项目扫描记录',
  '{count} project scan notes': '{count} 条项目扫描记录',
  'ELIGIBLE LIBRARY': '可用库',
  'Search Skills': '搜索 Skill',
  'Find by name, tag, or description': '按名称、标签或描述查找',
  Search: '搜索',
  'Searching eligible Skills…': '正在搜索可用 Skill…',
  'No tags': '无标签',
  'No eligible Skills found': '没有找到可用 Skill',
  'Try another term or add a valid SKILL.md to the configured library.': '请尝试其他关键词，或在已配置的库中添加有效的 SKILL.md。',
  'SKILL DETAIL / {identifier}': 'SKILL 详情 / {identifier}',
  'Local library': '本地库',
  Configured: '已配置',
  Unavailable: '不可用',
  Unsupported: '不受支持',
  Detected: '已检测',
  Managed: '已托管',
  Unmanaged: '未托管',
  Orphaned: '来源缺失',
  Tracked: '已跟踪',
  Ignored: '已忽略',
  'Would be tracked': '将被跟踪',
  High: '高',
  Medium: '中',
  Low: '低',
  'Skill resource': 'Skill 资源',
  'SubAgent resource': '子代理资源',
  'Matched project evidence: {evidence}': '与项目标记匹配：{evidence}',
  Tags: '标签',
  None: '无',
  Compatibility: '兼容性',
  Any: '不限',
  'Not declared': '未声明',
  'SKILL.MD CONTENT': 'SKILL.MD 内容',
  'READ ONLY · NEVER EXECUTED': '只读 · 不会执行',
  'Select a Skill to read its content.': '选择一个 Skill 以查看其内容。',
  'Allow replacement of existing conflicts': '允许替换现有冲突',
  'Preparing preview…': '正在准备预览…',
  'Select a Skill': '选择一个 Skill',
  'Open a catalog entry to inspect its metadata and plan a guarded installation.': '打开目录条目以查看元数据并制定受保护的安装方案。',
  'Reading canonical definitions…': '正在读取规范定义…',
  'SubAgents are unavailable': '子代理不可用',
  'Try refreshing this view.': '请尝试刷新此视图。',
  'CANONICAL DEFINITIONS': '规范定义',
  'Skills ': '技能 ',
  'Agents ': 'Agent ',
  'Capabilities ': '能力 ',
  'No canonical SubAgents': '没有规范子代理',
  'Definitions are read from the configured Agent Manager data root.': '定义从已配置的 Agent Manager 数据根目录读取。',
  'VALIDATION NOTES': '校验记录',
  'Invalid definition': '定义无效',
  'All definitions are valid': '所有定义均有效',
  'SubAgent inspection is read only; this view has no installation controls.': '子代理检查为只读；此视图不提供安装操作。',
  'Doctor findings': 'Doctor 检查结果',
  'Project scan notes': '项目扫描记录',
  'Run scan': '运行扫描',
  'Available repairs are previewed, require confirmation, and are recorded for Undo.': '可用修复会先显示预览，须经确认后执行，并记录以供撤销。',
  'Repair available': '可修复',
  'Manual review': '需人工检查',
  'Preview reconciliation': '预览重新关联',
  'Add ignore rule': '添加忽略规则',
  'No Doctor findings': 'Doctor 未发现问题',
  'The Doctor scan completed without findings.': 'Doctor 检查完成，未发现问题。',
  'No project scan notes': '没有项目扫描记录',
  'The static project scan completed without notes.': '静态项目扫描完成，没有扫描记录。',
  'Scanning configured resources…': '正在扫描已配置资源…',
  'No findings': '没有发现问题',
  'The current Doctor scan returned no issues.': '本次 Doctor 检查未发现问题。',
  'LATEST REVERSIBLE OPERATION': '最近一次可撤销操作',
  'PROJECT JOURNAL': '项目日志',
  'Review Undo': '审查撤销方案',
  'No operation journal yet': '暂无操作日志',
  'Successful guarded operations appear here with an Undo preview.': '成功执行的受保护操作及其撤销预览会显示在这里。',
  'Reading operation journal…': '正在读取操作日志…',
  '{count} paths in the available Undo preview.': '可撤销预览涉及 {count} 个路径。',
  'REVIEW BEFORE APPLYING': '应用前请审查',
  'Expires {time}': '过期时间：{time}',
  'Force replacement confirmed for this plan. Existing conflicting content may be replaced.': '此方案已确认强制替换，现有冲突内容可能会被替换。',
  'Applying…': '正在应用…',
  'Confirm and apply': '确认并应用',
  'Cancel plan': '取消方案',
  'Could not load this view': '无法加载此视图',
  'Select console language': '选择控制台语言',
  'English': 'English',
  'Simplified Chinese': '简体中文',
  'Registered {project}.': '已注册 {project}。',
  '{operation} completed and journaled.': '已完成 {operation}，并写入操作日志。',
  'The pending plan was cancelled.': '待处理方案已取消。',
  'An unexpected local service error occurred.': '本地服务发生意外错误。',
  'Request failed with status {status}.': '请求失败，HTTP 状态码为 {status}。',
  'Console language': '控制台语言',
  'Storage & Sync': '存储与同步',
  'CENTRAL SKILL LIBRARY': '中央 Skill 库',
  'SOURCE INVENTORY': '来源清单',
  'EXISTING ACTIVATIONS': '现有启用项',
  'GLOBAL SOURCES': '全局来源',
  'PROJECT SOURCES': '项目来源',
  'IMPORT PREVIEW': '导入预览',
  'RESYNC PREVIEW': '重同步预览',
  'OPERATION UNDO': '操作撤销',
  Global: '全局',
  Project: '项目',
  'Repository path': 'Skill 库路径',
  'How new activations are synchronized': '新启用项的同步方式',
  'Save sync mode': '保存同步方式',
  Automatic: '自动：优先使用符号链接，失败时复制（推荐）',
  'Symlink only': '仅使用符号链接',
  'Copy only': '仅复制',
  'Try a symbolic link first, then copy if links are unavailable.': '优先创建符号链接；系统不支持时复制 Skill 文件。',
  'Use symbolic links. Link errors are shown for review.': '只创建符号链接；无法创建时会显示错误。',
  'Copy Skill files into the agent location.': '将 Skill 文件复制到 Agent 目录。',
  'Imported Skills are copied into the Agent Manager repository. Their source folders stay unchanged, and importing does not activate them.': '导入的 Skill 会复制到 Agent Manager 库中。来源目录保持不变，导入也不会自动启用 Skill。',
  'Refresh sources': '刷新来源',
  'RECORDED UPSTREAM SOURCES': '已记录的上游来源',
  'Check for updates': '检查更新',
  'Check installed Skill updates': '检查已安装 Skill 的更新',
  'Only this button contacts supported GitHub sources recorded by Skills CLI. Local and unsupported sources are shown without a network check.': '只有点击此按钮才会访问 Skills CLI 记录的受支持 GitHub 来源。本地和暂不支持的来源只展示信息，不会联网检查。',
  'Last checked: {time}': '上次检查：{time}',
  'No Skills with recorded sources': '没有带来源记录的 Skill',
  'Refresh the source inventory after installing a Skill with Skills CLI.': '使用 Skills CLI 安装 Skill 后，请刷新来源清单。',
  'Update available': '有可用更新',
  'Up to date': '已是最新',
  'Locally modified': '本机内容已修改',
  'Cannot check': '暂不支持检查',
  'Check failed': '检查失败',
  'Scope: {scope}': '范围：{scope}',
  'Ref: {ref}': '分支或版本：{ref}',
  'Current hash: {hash}': '当前校验值：{hash}',
  'New hash: {hash}': '上游校验值：{hash}',
  'Select update for {identifier}': '选择更新 {identifier}',
  'Update all available': '更新全部可用项',
  'Update selected': '更新所选项',
  'Undo latest Skill update': '撤销最近一次 Skill 更新',
  'UPSTREAM UPDATE PREVIEW': '上游更新预览',
  'UPDATE UNDO': '撤销更新',
  '{count} Skills': '{count} 个 Skill',
  'Reviewed restoration': '已审查的恢复操作',
  'Review Skill updates': '审查 Skill 更新内容',
  'Review the latest upstream update Undo': '审查最近一次上游更新的撤销内容',
  'Review each source path and hash. Files change only after you confirm.': '请核对每个来源路径和校验值。只有确认后才会修改文件。',
  'Undo restores the Skill files and source lock metadata captured before the update.': '撤销会恢复更新前保存的 Skill 文件和来源锁记录。',
  'Confirm Skill updates': '确认更新 Skill',
  'Confirm update Undo': '确认撤销更新',
  '{count} Skills updated from their recorded sources.': '已从记录的来源更新 {count} 个 Skill。',
  'The latest upstream Skill update was undone.': '最近一次上游 Skill 更新已撤销。',
  'Only hash-tracked GitHub Skills are supported': '目前只支持带哈希记录的 GitHub Skill',
  'Installed files differ from the recorded install hash': '本机文件与安装记录中的校验值不同',
  'The installed Skill content could not be safely compared': '无法安全比较已安装的 Skill 内容',
  'Project lock hashes can only be compared safely for single-file Skills': '项目锁文件的校验值目前只支持安全比较单文件 Skill',
  'Skills ready to import': '可导入的 Skills',
  'Choose a version to add it to the central library. Matching content from several locations is grouped once.': '选择要加入中央库的版本。多个位置中内容相同的 Skill 会合并显示。',
  'Select all': '全选',
  'Select {identifier}': '选择 {identifier}',
  'Source': '来源',
  'Source: {source}': '来源：{source}',
  'Local source': '本地来源',
  'Unknown source': '未知来源',
  'Local modified: {time}': '本机修改时间：{time}',
  'Local modification time unavailable': '无法读取本机修改时间',
  'Newest local modification: {time}': '最近本机修改时间：{time}',
  'Local modification time is newer': '本机修改时间较新',
  'Local timestamps are comparison hints only; choose the version you want to keep.': '本机修改时间仅供比较参考；请根据内容选择要保留的版本。',
  'Content checksum: {checksum}': '内容校验值：{checksum}',
  'Selected as current version': '已选为当前版本',
  'Selected for current version; preview not updated': '已选为当前版本；尚未更新预览',
  'Current-version choice is included in preview': '当前版本选择已加入预览',
  'No eligible global source Skills were found.': '未找到符合条件的全局 Skill 来源。',
  'This project has no eligible source Skills.': '此项目中没有符合条件的 Skill 来源。',
  'Same content appears in {count} locations': '相同内容出现在 {count} 个位置',
  'Already managed as {method}': '已通过{method}托管',
  'Not eligible · {problem}': '不符合导入条件 · {problem}',
  'Review selected import': '审查所选导入项',
  'Review before importing': '导入前请审查',
  '{count} selected versions': '已选 {count} 个版本',
  'New version': '新版本',
  Conflict: '冲突',
  'Already in repository': '已在库中',
  'Current version stays {version}': '当前版本保持为 {version}',
  'Will become the current version': '将设为当前版本',
  'Choose the current version for {identifier}': '为 {identifier} 选择当前版本',
  'Update preview with this choice': '按此选择更新预览',
  'Creating import preview…': '正在创建导入预览…',
  'Updating import preview…': '正在更新导入预览…',
  'Updating preview…': '正在更新预览…',
  'Importing…': '正在导入…',
  'Choose a candidate, then update the preview before importing.': '选择一个候选版本，再更新预览，然后确认导入。',
  'This choice is already included in the preview.': '当前预览已包含此选择。',
  'Complete the current-version choices for {skills}: select each candidate and update the preview before confirming.': '请完成 {skills} 的当前版本选择：为每项选择候选版本并更新预览，然后再确认导入。',
  'Resolve the repository conflicts for {skills} before importing.': '请先解决 {skills} 的 Skill 库冲突，再导入。',
  'All selected versions are already in the repository; there is nothing to import.': '所选版本已全部存在于 Skill 库，无需导入。',
  'The preview is current and ready for confirmation.': '预览已包含当前选择，请检查后确认导入。',
  'Versions already in the repository will be skipped.': '库中已有的版本会跳过。',
  'Confirm import': '确认导入',
  Cancel: '取消',
  'Scope by scope': '按范围分别处理',
  'Resync managed Skills': '重同步托管 Skills',
  'Apply the saved synchronization mode to journal-managed activations. Each scope has its own review, operation journal, and Undo.': '将已保存的同步方式应用到日志记录的托管启用项。全局和项目范围分别审查、记录操作并支持撤销。',
  'Review global resync': '审查全局重同步',
  'Review project resync': '审查项目重同步',
  'Global Undo': '撤销全局操作',
  'Project Undo': '撤销项目操作',
  'Review managed destinations': '审查托管目标',
  'Review the latest scope operation': '审查此范围的最近操作',
  'No managed activations to resync': '没有可重同步的托管启用项',
  'This scope has no journal-managed Skills.': '此范围没有操作日志记录的托管 Skill。',
  'Confirm resync': '确认重同步',
  'Confirm scope Undo': '确认撤销此范围操作',
  'Sync mode saved. Resync a scope to update existing activations.': '同步方式已保存。重同步某个范围后，现有启用项才会更新。',
  '{count} Skill versions imported. Sources were left in place.': '已导入 {count} 个 Skill 版本，来源目录保持不变。',
  '{scope} activations resynchronized.': '已重同步{scope}启用项。',
  '{scope} resync was undone.': '已撤销{scope}重同步。',
  'Reading supported Skill locations…': '正在读取受支持的 Skill 位置…',
  'Storage & Sync is unavailable': '存储与同步不可用',
  'The local synchronization inventory could not be loaded.': '无法读取本地同步清单。',
  'Local workspace manager for agent resources.': '用于管理 Agent 资源的本地工作区。',
}

const messageCatalog: Record<string, string> = {
  'scan.traversal_limit': '扫描在检查 {limit} 个目录条目后停止',
  'scan.unreadable_directory': '无法读取目录：{detail}',
  'scan.skipped_directory_symlink': '已跳过目录符号链接以避免重复遍历',
  'scan.unreadable_marker': '无法读取标记文件：{detail}',
  'scan.marker_read_limit': '标记文件超过 1 MiB 静态读取上限，未进行解析',
  'doctor.invalid_catalog_entry': '目录条目无效：{detail}',
  'doctor.unmanaged_resource': '未托管的资源',
  'doctor.orphaned_managed_link': '托管链接的目标不存在：{target}',
  'doctor.ambiguous_dangling_link': '悬空链接的归属不明确：拒绝自动协调',
  'doctor.unsupported_skill_location': '不受支持的 Agent Skill 位置',
  'doctor.git_worktree_unavailable': 'Git 无法检查此工作树',
  'doctor.git_managed_link_status': 'Git 提示：托管绝对路径链接状态为 {status}；确认后将此确切托管路径添加到 .gitignore',
  'doctor.guidance.invalid_catalog_entry': '修复已配置库中的无效 Skill 条目，然后重新扫描。',
  'doctor.guidance.unmanaged_resource': '此资源不由 Agent Manager 托管，因此未作修改。决定接管或移除前请先检查。',
  'doctor.guidance.orphaned_unrepairable': '找不到可用于恢复此托管链接的有效 Skill。请恢复或更正已配置的库条目，然后重新扫描。',
  'doctor.guidance.ambiguous_dangling_link': '无法确认此悬空链接的归属。修改前请检查目标和所有权。',
  'doctor.guidance.unsupported_skill_location': '此 Agent Skill 位置不支持托管操作。请将资源移至受支持的位置，或在那里管理。',
  'doctor.guidance.git_worktree_unavailable': 'Git 无法检查此工作树。请初始化或修复工作树，然后重新运行 Doctor。',
  'doctor.guidance.gitignore_unavailable': '无法安全生成精确忽略规则。请检查项目 .gitignore 和托管链接，然后重新扫描。',
  'doctor.guidance.git_tracked': '此链接已在 Git 索引中；添加忽略规则不会将其取消跟踪，请手动检查索引。',
  'doctor.guidance.git_ignored': '此托管链接已被 Git 忽略。',
  'recommendation.matched_evidence': '与项目标记匹配：{evidence}',
  'plan.operation.activate': '启用',
  'plan.operation.activate_selected_skills': '启用所选 Skills',
  'plan.operation.resynchronize_managed_skills': '重同步托管 Skills',
  'plan.operation.remove': '移除',
  'plan.operation.reconcile': '重新关联',
  'plan.operation.gitignore': '更新托管链接的 Git 忽略规则',
  'plan.operation.undo': '撤销',
  'plan.operation.undo_action': '撤销{operation}',
  'plan.action.create_link': '创建绝对路径链接',
  'plan.action.replace_conflict': '将冲突路径替换为绝对路径链接',
  'plan.action.remove_managed_link': '移除托管链接',
  'plan.action.restore_previous_state': '恢复操作前状态',
  'plan.action.refuse_conflict': '拒绝冲突目标',
  'plan.action.replace_conflict_force_required': '替换冲突（需要强制确认）',
  'plan.action.copy_skill_to_library': '将项目 Skill 复制到库中',
  'plan.action.replace_with_managed_link': '将目录替换为托管链接',
  'plan.action.repoint_orphaned_link': '重新指向失效的托管链接',
  'plan.action.gitignore_managed_link': '忽略托管链接',
  'plan.warning.compatibility_not_declared': '{skill} 未声明与 {target} 兼容',
  'plan.warning.already_installed': '资源已安装；未进行任何更改',
}

const apiErrors: Record<string, string> = {
  unauthorized: '需要有效的控制台会话令牌。',
  untrusted_host: '请求必须使用当前本地回环控制台来源。',
  untrusted_origin: '不允许此请求来源。',
  origin_required: '更改状态的请求必须来自控制台来源。',
  cross_site_request: '不允许跨站请求。',
  method_not_allowed: '此路由不支持该请求方法。',
  project_already_registered: '此控制台会话已注册另一个项目。',
  stale_project: '已注册的项目目录已更改。请重启控制台后重新注册。',
    invalid_project: '项目目录无效：{detail}',
  not_found: '找不到此 API 路由。',
  invalid_content_type: '请求必须使用 application/json。',
  request_too_large: 'JSON 请求超过 64 KiB 限制。',
  invalid_json: '请求正文必须是一个仅包含受支持字段的有效 JSON 对象。',
  project_not_found: '此控制台会话中未注册该项目。',
  plan_creation_failed: '无法创建安全的操作方案。',
  plan_not_found: '此会话中不存在该操作方案。',
  plan_expired: '操作方案已过期，请创建新的预览。',
  plan_cancelled: '操作方案已取消。',
  plan_consumed: '操作方案已执行。',
  stale_plan: '操作方案已不是最新状态，请检查项目并创建新的预览。',
  plan_executing: '操作方案正在执行。',
  journal_committed: '操作已应用并写入日志，但同步日志目录时失败。项目状态未回滚；请刷新后再创建其他方案。',
  operation_conflict: '操作状态已更改。请检查项目和操作日志后再重试。',
  operation_failed: '操作失败。请检查项目和操作日志后再重试。',
  plan_not_pending: '只有待处理的操作方案可以取消。',
  inventory_failed: '无法读取 Agent 清单。',
  recommendations_failed: '无法读取项目依据或已配置的 Skill 目录。',
  unsafe_journal_path: '项目操作日志路径不是安全的本地目录。',
  unsafe_journal_entry: '项目操作日志包含超出已注册安全边界的路径。',
  settings_unavailable: '无法读取 Agent Manager 同步设置。',
  settings_write_failed: '无法保存同步方式。',
  invalid_sync_mode: '同步方式无效，请选择支持的选项。',
  plan_unavailable: '无法保存同步审查方案。',
  undo_unavailable: '当前范围没有可用的撤销操作。',
  project_required: '请先注册项目。',
  invalid_scope: '同步范围无效。',
  invalid_sync_request: '同步请求无效或包含不受支持的数据。',
  sync_failed: '同步操作失败。请检查操作日志后重试。',
  diagnostics_failed: '无法完成 Doctor 诊断。',
  journal_unreadable: '无法读取项目操作日志。',
  catalog_unreadable: '无法读取已配置的 Skill 目录。',
  skill_not_found: '找不到具有该标识符的可用 Skill。',
  subagents_unavailable: '无法读取规范子代理定义。',
  subagent_not_found: '找不到具有该标识符的有效规范子代理。',
  invalid_plan: '操作方案无效。',
  nothing_to_undo: '没有可撤销的操作。',
  request_failed: '请求失败。',
}

const apiErrorDetails: Record<string, Record<string, string>> = {
  stale_plan: {
    'The operation plan is no longer current; create a fresh preview.': '操作方案已不是最新状态，请创建新的预览。',
    'The registered project changed; create a fresh preview.': '已注册的项目已更改，请创建新的预览。',
    'Project, library, or journal state changed after review; create a fresh preview.': '审查后项目、库或日志状态已更改，请创建新的预览。',
    'Operation state changed during execution. Inspect the project and operation journal before creating a fresh preview.': '执行期间操作状态发生更改。请检查项目和操作日志，再创建新的预览。',
  },
  plan_expired: {
    'The operation plan expired while waiting for another local operation; create a fresh preview.': '等待其他本地操作期间，操作方案已过期；请创建新的预览。',
    'The operation plan expired and can no longer be cancelled or executed.': '操作方案已过期，无法再取消或执行。',
  },
}

function interpolate(template: string, values?: Values) {
  return template.replace(/\{(\w+)\}/g, (match, name: string) => {
    const value = values?.[name]
    return value === undefined ? match : String(value)
  })
}

function browserLocale(): Locale {
  const languages = typeof navigator === 'undefined' ? [] : navigator.languages?.length ? navigator.languages : [navigator.language]
  return languages.some((language) => /^zh(?:-|$)/i.test(language)) ? 'zh-CN' : 'en'
}

function savedLocale(): Locale | null {
  try {
    const value = window.localStorage.getItem(preferenceKey) ?? window.localStorage.getItem(previousInstallPreferenceKey)
    return value === 'en' || value === 'zh-CN' ? value : null
  } catch {
    return null
  }
}

const LanguageContext = createContext<LanguageContextValue | null>(null)

export function LanguageProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(() => savedLocale() ?? browserLocale())
  const setLocale = (next: Locale) => {
    setLocaleState(next)
    try {
      window.localStorage.setItem(preferenceKey, next)
    } catch {
      // The current selection remains active for this page when storage is unavailable.
    }
  }
  const value = useMemo<LanguageContextValue>(() => {
    const t = (source: string, values?: Values) => interpolate(locale === 'zh-CN' ? zh[source] ?? source : source, values)
    const installationMessage = (source: string) => {
      if (locale === 'en') return source
      const compatibility = source.match(/^(.+) is not declared compatible with (claude-code|codex|pi)$/)
      if (compatibility) {
        const target = { 'claude-code': 'Claude Code', codex: 'Codex', pi: 'Pi' }[compatibility[2]] ?? compatibility[2]
        return t('Skill "{skill}" is not declared compatible with {target}', { skill: compatibility[1], target })
      }
      const missingSkill = source.match(/^Skill "(.+)" not found in configured library$/)
      if (missingSkill) return `在配置的技能库中未找到技能“${missingSkill[1]}”`
      return t(source)
    }
    const message = (ref: MessageRef | undefined, fallback: string) => {
      if (!ref) return fallback
      if (locale === 'en') return fallback
      const template = messageCatalog[ref.key]
      if (!template) return fallback
      const args = ref.args ? { ...ref.args } : undefined
      if (ref.key === 'plan.operation.undo_action' && args?.operation) {
        args.operation = messageCatalog[args.operation] ?? args.operation
      }
      const localized = interpolate(template, args)
      return /\{\w+\}/.test(localized) ? fallback : localized
    }
  const error = (value: unknown) => {
      if (value instanceof APIError) {
        if (value.code === 'installation_error') return installationMessage(value.message)
        const translated = locale === 'zh-CN' ? apiErrors[value.code] : undefined
        if (!translated) return value.message
        const detail = apiErrorDetails[value.code]?.[value.message]
        if (detail) return detail
        if (value.code === 'invalid_project') return interpolate(translated, { detail: value.message })
        if (value.code === 'request_failed') return t('Request failed with status {status}.', { status: value.status })
        return translated
      }
      if (value instanceof Error) return value.message === 'Installation could not be completed.' ? t(value.message) : value.message
      return t('An unexpected local service error occurred.')
    }
    return { locale, setLocale, t, message, installationMessage, error }
  }, [locale])

  useEffect(() => {
    document.documentElement.lang = locale
    document.title = locale === 'zh-CN' ? 'Agent Manager · 本地控制台' : 'Agent Manager · Local console'
    const description = document.querySelector<HTMLMetaElement>('meta[name="description"]')
    if (description) description.content = locale === 'zh-CN' ? zh['Local workspace manager for agent resources.'] : 'A local workspace for managing agent resources.'
  }, [locale])

  return <LanguageContext.Provider value={value}>{children}</LanguageContext.Provider>
}

export function useLanguage() {
  const value = useContext(LanguageContext)
  if (!value) throw new Error('LanguageProvider is missing.')
  return value
}

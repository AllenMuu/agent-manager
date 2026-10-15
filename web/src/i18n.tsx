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
  'The console inspects this directory through Agent Manager’s existing read-only services. It does not expose general file browsing.': '控制台通过 Agent Manager 现有的只读服务检查此目录，不提供通用文件浏览功能。',
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
  'Doctor findings are available in a read-only view.': '可在只读视图中查看 Doctor 检查结果。',
  'Open diagnostics': '打开诊断',
  'Recommended Skills': '推荐 Skill',
  'RECOMMENDED SKILLS': '推荐 Skill',
  'Retry recommendations': '重试推荐',
  'Eligible library': '可用库',
  'Skill detail': 'Skill 详情',
  'Read details →': '查看详情 →',
  'Select {identifier}': '选择 {identifier}',
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
  'Activation targets': '启用目标',
  'Allow replacement of existing conflicts': '允许替换现有冲突',
  'Requires this separate confirmation and is recorded in the review plan.': '此操作需要单独确认，并会记录在审查方案中。',
  'Preparing preview…': '正在准备预览…',
  'Review activation plan': '审查启用方案',
  'Select a Skill': '选择一个 Skill',
  'Open a catalog entry to inspect its metadata and plan a guarded activation.': '打开目录条目以查看元数据并制定受保护的启用方案。',
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
  'READ-ONLY DOCTOR': '只读 Doctor 检查',
  'Run scan': '运行扫描',
  'Findings come from the existing doctor checks. This scan never reconciles project state.': '结果来自现有 Doctor 检查。此扫描不会协调或修改项目状态。',
  'Scanning configured resources…': '正在扫描已配置资源…',
  'No findings': '没有发现问题',
  'The current read-only doctor scan returned no issues.': '本次只读 Doctor 扫描未发现问题。',
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
  'Skill installation': '技能安装',
  'Local workspace manager for agent resources.': '用于管理 Agent 资源的本地工作区。',
}

const messageCatalog: Record<string, string> = {
  'scan.traversal_limit': '扫描在检查 {limit} 个目录条目后停止',
  'scan.unreadable_directory': '无法读取目录：{detail}',
  'scan.skipped_directory_symlink': '已跳过目录符号链接以避免重复遍历',
  'scan.unreadable_marker': '无法读取标记文件：{detail}',
  'scan.marker_read_limit': '标记文件超过 1 MiB 静态读取上限，未进行解析',
  'doctor.invalid_catalog_entry': '目录条目无效：{detail}',
  'doctor.adapter_capabilities': '适配器能力：{target} 支持 {kind}（{capabilities}）',
  'doctor.unmanaged_resource': '未托管的资源',
  'doctor.orphaned_managed_link': '托管链接的目标不存在：{target}',
  'doctor.ambiguous_dangling_link': '悬空链接的归属不明确：拒绝自动协调',
  'doctor.unsupported_skill_location': '不受支持的 Agent Skill 位置',
  'doctor.git_worktree_unavailable': 'Git 提示：不要跟踪托管的绝对路径链接；请初始化或修复此 Git 工作树以检查准确状态',
  'doctor.git_managed_link_status': 'Git 提示：托管绝对路径链接状态为 {status}；确认后将此确切托管路径添加到 .gitignore',
  'recommendation.matched_evidence': '与项目标记匹配：{evidence}',
  'plan.operation.activate': '启用',
  'plan.operation.activate_selected_skills': '启用所选 Skills',
  'plan.operation.remove': '移除',
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
  diagnostics_failed: '无法完成只读诊断。',
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

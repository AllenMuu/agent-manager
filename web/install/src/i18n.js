export const languageStorageKey = 'agent-manager.console-language'
const previousLanguageStorageKey = 'agent-manager.webui.language'

const messages = {
  en: {
    language: 'Language',
    english: 'English',
    chinese: 'Simplified Chinese',
    home: 'Agent Manager home',
    localProject: 'Local project',
    skillLibrary: 'SKILL LIBRARY',
    projectActivation: 'PROJECT ACTIVATION',
    headline: 'Choose what your agents need.',
    intro: 'Browse your local library, review project matches, then preview the exact files before installing.',
    readingProject: 'Reading project…',
    dismiss: 'Dismiss',
    projectMatches: 'PROJECT MATCHES',
    recommended: 'Recommended for this project',
    readOnly: 'Read-only analysis',
    noMatches: 'No catalog matches yet. Search the library below to choose Skills manually.',
    scanning: 'Scanning project markers…',
    localLibrary: 'LOCAL LIBRARY',
    browseSkills: 'Browse Skills',
    search: 'Search by name, description, or tag',
    noSkills: 'No eligible Skills found in the configured local library.',
    installSelection: 'INSTALL SELECTION',
    chooseTargets: 'Choose target agents',
    selected: 'selected',
    selectSkill: 'Select at least one Skill to continue.',
    selectTarget: 'Select one or more target agents.',
    reviewChanges: 'You will review the file changes before installation.',
    working: 'Working…',
    previewInstall: 'Preview installation',
    reviewBeforeInstall: 'REVIEW BEFORE INSTALLING',
    fileChanges: 'file changes',
    allowReplacement: 'Allow replacing the conflicting destination',
    forceRecorded: 'This force option is recorded in the preview. Review the replacement plan before confirming.',
    replacementEnabled: 'Replacement is explicitly enabled. Confirm only after reviewing the paths above.',
    nothingUntilConfirm: 'Nothing changes until you confirm this plan.',
    previewReplacement: 'Preview replacement',
    applying: 'Applying…',
    confirmInstall: 'Confirm and install',
    runsLocally: 'Runs on this device · No Skill code is executed',
    match: 'MATCH',
    compatibilityUndeclared: 'Compatibility not declared',
    installed: 'Selected Skills installed successfully. The operation is recorded and can be undone with the CLI.',
    stalePreview: 'The project changed after preview. Review this updated plan, then confirm again.',
    replacementPreview: 'Replacement preview requested. Review the affected paths before confirming.',
    readLibraryError: 'Could not read the local Skill library.',
    scanProjectError: 'Could not scan this project.',
    createPreviewError: 'Could not create an installation preview.',
    createPreviewRetry: 'Could not create an installation preview. Check that the local service is running and retry.',
    installError: 'Installation could not be completed.',
    installRetry: 'Installation could not be completed. Check that the local service is running and retry.',
    cannotReachService: 'Could not reach the local Agent Manager service. Check that it is still running and retry.',
    activateSelectedSkills: 'activate selected skills',
    createAbsoluteLink: 'create absolute link',
    replaceConflictingLink: 'replace conflicting path with absolute link',
    refuseConflict: 'refuse conflicting destination',
    replaceForceRequired: 'replace conflict (force required)',
    alreadyInstalled: 'resource is already installed; no changes made',
    invalidRequest: 'invalid installation request',
    noChanges: 'no installation changes',
    previewExpired: 'installation preview expired or unavailable',
    forceRequired: 'conflict strategy requires force confirmation',
    unsafePath: 'refusing unmanaged or unexpected path',
  },
  'zh-CN': {
    language: '语言',
    english: 'English',
    chinese: '简体中文',
    home: 'Agent Manager 首页',
    localProject: '本地项目',
    skillLibrary: '技能库',
    projectActivation: '项目启用',
    headline: '为你的智能体选择所需技能。',
    intro: '浏览本地技能库，查看与项目的匹配项，并在安装前预览具体文件变更。',
    readingProject: '正在读取项目…',
    dismiss: '关闭提示',
    projectMatches: '项目匹配',
    recommended: '为此项目推荐',
    readOnly: '只读分析',
    noMatches: '暂未找到匹配项。你可以在下方搜索技能库并手动选择。',
    scanning: '正在扫描项目标记…',
    localLibrary: '本地技能库',
    browseSkills: '浏览技能',
    search: '按名称、描述或标签搜索',
    noSkills: '配置的本地技能库中没有可用技能。',
    installSelection: '安装所选技能',
    chooseTargets: '选择目标智能体',
    selected: '项已选择',
    selectSkill: '请至少选择一项技能以继续。',
    selectTarget: '请选择一个或多个目标智能体。',
    reviewChanges: '安装前你将先检查文件变更。',
    working: '处理中…',
    previewInstall: '预览安装',
    reviewBeforeInstall: '安装前检查',
    fileChanges: '项文件变更',
    allowReplacement: '允许替换发生冲突的目标路径',
    forceRecorded: '此强制选项会记录在预览中。确认前请检查替换计划。',
    replacementEnabled: '已明确启用替换。请先检查上方路径，再确认操作。',
    nothingUntilConfirm: '确认此计划前不会更改任何内容。',
    previewReplacement: '预览替换方案',
    applying: '正在应用…',
    confirmInstall: '确认并安装',
    runsLocally: '在此设备运行 · 不会执行技能代码',
    match: '匹配',
    compatibilityUndeclared: '未声明兼容性',
    installed: '所选技能已成功安装。此操作已记录，可使用 CLI 撤销。',
    stalePreview: '预览后项目发生了变化。请检查更新后的计划并再次确认。',
    replacementPreview: '已请求预览替换方案。请在确认前检查受影响的路径。',
    readLibraryError: '无法读取本地技能库。',
    scanProjectError: '无法扫描此项目。',
    createPreviewError: '无法创建安装预览。',
    createPreviewRetry: '无法创建安装预览。请确认本地服务仍在运行后重试。',
    installError: '无法完成安装。',
    installRetry: '无法完成安装。请确认本地服务仍在运行后重试。',
    cannotReachService: '无法连接本地 Agent Manager 服务。请确认服务仍在运行后重试。',
    activateSelectedSkills: '启用所选技能',
    createAbsoluteLink: '创建绝对路径链接',
    replaceConflictingLink: '用绝对路径链接替换冲突路径',
    refuseConflict: '拒绝替换冲突目标',
    replaceForceRequired: '替换冲突项（需要强制确认）',
    alreadyInstalled: '资源已安装；未进行更改',
    invalidRequest: '安装请求无效',
    noChanges: '没有需要执行的安装变更',
    previewExpired: '安装预览已过期或不可用',
    forceRequired: '替换冲突项需要明确的强制确认',
    unsafePath: '为保护数据，已拒绝操作非托管或异常路径',
  },
}

const exactMessages = new Map([
  ['Could not read the local Skill library.', 'readLibraryError'],
  ['Could not scan this project.', 'scanProjectError'],
  ['Could not create an installation preview.', 'createPreviewError'],
  ['Could not create an installation preview. Check that the local service is running and retry.', 'createPreviewRetry'],
  ['Installation could not be completed.', 'installError'],
  ['Installation could not be completed. Check that the local service is running and retry.', 'installRetry'],
  ['Selected Skills installed successfully. The operation is recorded and can be undone with the CLI.', 'installed'],
  ['The project changed after preview. Review this updated plan, then confirm again.', 'stalePreview'],
  ['Replacement preview requested. Review the affected paths before confirming.', 'replacementPreview'],
  ['Could not reach the local Agent Manager service. Check that it is still running and retry.', 'cannotReachService'],
  ['activate selected skills', 'activateSelectedSkills'],
  ['create absolute link', 'createAbsoluteLink'],
  ['replace conflicting path with absolute link', 'replaceConflictingLink'],
  ['refuse conflicting destination', 'refuseConflict'],
  ['replace conflict (force required)', 'replaceForceRequired'],
  ['resource is already installed; no changes made', 'alreadyInstalled'],
  ['invalid installation request', 'invalidRequest'],
  ['no installation changes', 'noChanges'],
  ['installation preview expired or unavailable', 'previewExpired'],
  ['conflict strategy requires force confirmation', 'forceRequired'],
  ['refusing unmanaged or unexpected path', 'unsafePath'],
])

export function initialLanguage() {
  try {
    const saved = globalThis.localStorage?.getItem(languageStorageKey) ?? globalThis.localStorage?.getItem(previousLanguageStorageKey)
    if (saved === 'en' || saved === 'zh-CN') return saved
  } catch {
    // Storage can be disabled by the browser; use language detection instead.
  }

  const preferred = globalThis.navigator?.languages?.[0] ?? globalThis.navigator?.language ?? ''
  return /^zh(?:-|$)/i.test(preferred) ? 'zh-CN' : 'en'
}

export function saveLanguage(language) {
  try {
    globalThis.localStorage?.setItem(languageStorageKey, language)
  } catch {
    // Keep the in-memory selection when storage is unavailable.
  }
}

export function translate(key, language) {
  return messages[language]?.[key] ?? messages.en[key] ?? key
}

export function presentMessage(message, language) {
  if (language !== 'zh-CN' || typeof message !== 'string') return message
  const exactKey = exactMessages.get(message)
  if (exactKey) return translate(exactKey, language)

  const compatibility = message.match(/^(.+) is not declared compatible with (claude-code|codex|pi)$/)
  if (compatibility) {
    const target = { 'claude-code': 'Claude Code', codex: 'Codex', pi: 'Pi' }[compatibility[2]]
    return `技能“${compatibility[1]}”未声明与 ${target} 兼容`
  }

  const missingSkill = message.match(/^Skill "(.+)" not found in configured library$/)
  if (missingSkill) return `在配置的技能库中未找到技能“${missingSkill[1]}”`

  const matchReason = message.match(/^matched (.+)$/)
  if (matchReason) {
    const fields = { tags: '标签', identifier: '标识符', description: '描述', body: '正文' }
    const details = matchReason[1].split(', ').map((part) => {
      const detail = part.match(/^(.+) via (tags|identifier|description|body)$/)
      return detail ? `${detail[1]}（${fields[detail[2]]}）` : null
    })
    if (details.every(Boolean)) return `匹配依据：${details.join('、')}`
  }

  return message
}

export function targetLabel(id, fallback, language) {
  if (language !== 'zh-CN') return fallback
  return { 'claude-code': 'Claude Code', codex: 'Codex', pi: 'Pi' }[id] ?? fallback
}

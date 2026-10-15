import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { APIError, get, post } from './api'
import { getSkillSelectionAction, toggleSkillSelection, type SkillSelectionAction } from './skillSelection'
import { bootstrapSession } from './session'
import { useLanguage } from './i18n'
import type {
  Agent,
  Finding,
  LatestOperation,
  InstallationCatalog,
  InstallationPreview,
  InstallationApplyResult,
  MessageRef,
  OperationPlan,
  Project,
  RecommendationResult,
  Resource,
  Skill,
  SubAgent,
  SubAgentDiagnostic,
  SyncImportPreview,
  SyncInventory,
  SyncMode,
  SyncPlan,
  SkillUpdateCheck,
  SkillUpdateItem,
  SkillUpdatePreview,
  SyncSource,
  SyncSourceGroup,
} from './types'
import styles from './App.module.css'

type View = 'workspace' | 'skills' | 'subagents' | 'diagnostics' | 'operations' | 'sync'
type PageState<T> = { data: T | null; loading: boolean; error: unknown | null }
type PlanEnvelope = { plan: OperationPlan }
type PendingInstallationPreview = InstallationPreview & { inputGeneration: number }
type InstallationPreviewResult = { preview: InstallationPreview; error: unknown | null }
type Toast = { kind: 'success'; text: string; values?: Record<string, string | number> } | { kind: 'error'; error: unknown }

const navItems: { id: View; label: string; eyebrow: string }[] = [
  { id: 'workspace', label: 'Workspace', eyebrow: '01' },
  { id: 'skills', label: 'Skills', eyebrow: '02' },
  { id: 'subagents', label: 'SubAgents', eyebrow: '03' },
  { id: 'diagnostics', label: 'Diagnostics', eyebrow: '04' },
  { id: 'operations', label: 'Operations', eyebrow: '05' },
  { id: 'sync', label: 'Storage & Sync', eyebrow: '06' },
]

const initialPage = <T,>(): PageState<T> => ({ data: null, loading: false, error: null })

function blockedInstallationPreview(error: unknown): InstallationPreview | null {
  if (!(error instanceof APIError) || !isRecord(error.responseBody)) return null
  const raw = error.responseBody.preview
  if (!isRecord(raw) || !isRecord(raw.plan) || !Array.isArray(raw.skillIds) || !Array.isArray(raw.targets)) return null

  const changes = Array.isArray(raw.plan.changes)
    ? raw.plan.changes.flatMap((change) => isRecord(change) && typeof change.path === 'string' && typeof change.action === 'string'
      ? [{ path: change.path, action: change.action, ...(typeof change.detail === 'string' ? { detail: change.detail } : {}) }]
      : [])
    : null
  const warnings = Array.isArray(raw.plan.warnings)
    ? raw.plan.warnings.filter((warning): warning is string => typeof warning === 'string')
    : null

  return {
    id: typeof raw.id === 'string' ? raw.id : '',
    skillIds: raw.skillIds.filter((identifier): identifier is string => typeof identifier === 'string'),
    targets: raw.targets.filter((target): target is string => typeof target === 'string'),
    plan: {
      operation: typeof raw.plan.operation === 'string' ? raw.plan.operation : 'activate',
      changes,
      warnings,
    },
  }
}

async function requestInstallationPreview(token: string, payload: unknown): Promise<InstallationPreviewResult> {
  try {
    const result = await post<{ preview: InstallationPreview }>(token, '/api/install/preview', payload)
    return { preview: result.preview, error: null }
  } catch (error) {
    const preview = blockedInstallationPreview(error)
    if (!preview) throw error
    return { preview, error }
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

export function App() {
  const { t, message: translateMessage, error: formatError } = useLanguage()
  const [token] = useState(() => bootstrapSession())
  const [project, setProject] = useState<Project | null>(null)
  const [projectPath, setProjectPath] = useState('')
  const [view, setView] = useState<View>(() => /^\/install\/?$/.test(window.location.pathname) ? 'skills' : 'workspace')
  const [workspace, setWorkspace] = useState<PageState<{ agents: Agent[]; recommendations: RecommendationResult; resources: Resource[] }>>(initialPage)
  const [skills, setSkills] = useState<PageState<InstallationCatalog>>(initialPage)
  const [fullCatalog, setFullCatalog] = useState<PageState<InstallationCatalog>>(initialPage)
  const [subagents, setSubagents] = useState<PageState<{ definitions: SubAgent[]; diagnostics: SubAgentDiagnostic[] }>>(initialPage)
  const [findings, setFindings] = useState<PageState<Finding[]>>(initialPage)
  const [latest, setLatest] = useState<PageState<LatestOperation>>(initialPage)
  const [recommendations, setRecommendations] = useState<PageState<RecommendationResult>>(initialPage)
  const [selectedSkillIDs, setSelectedSkillIDs] = useState<string[]>([])
  const selectionAction: SkillSelectionAction = getSkillSelectionAction(selectedSkillIDs)
  const [query, setQuery] = useState('')
  const [selectedSkill, setSelectedSkill] = useState<Skill | null>(null)
  const [targets, setTargets] = useState<string[]>(['codex'])
  const [batchTargets, setBatchTargets] = useState<string[]>([])
  const [batchReplaceConflicts, setBatchReplaceConflicts] = useState(false)
  const [batchPreview, setBatchPreview] = useState<PendingInstallationPreview | null>(null)
  const [pendingActivation, setPendingActivation] = useState(false)
  const [forceReplace, setForceReplace] = useState(false)
  const [pendingPlan, setPendingPlan] = useState<OperationPlan | null>(null)
  const [toast, setToast] = useState<Toast | null>(null)
  const [busy, setBusy] = useState(false)
  const mutationRequestInFlight = useRef(false)
  const batchInputGeneration = useRef(0)
  const invalidateBatchPreview = useCallback(() => {
    batchInputGeneration.current += 1
    setBatchPreview(null)
  }, [])

  const availableSkillIDs = new Set(fullCatalog.data?.skills.map((skill) => skill.identifier))
  const unavailableSkillIDs = fullCatalog.data ? selectedSkillIDs.filter((id) => !availableSkillIDs.has(id)) : []
  const mutationBlocked = busy || skills.loading || !!skills.error || fullCatalog.loading || !!fullCatalog.error || !fullCatalog.data || unavailableSkillIDs.length > 0
  const batchTargetsValid = batchTargets.length > 0 && batchTargets.every((target) => fullCatalog.data?.targets.some((supported) => supported.id === target))

  const projectBase = project ? `/api/v1/projects/${project.id}` : ''

  const loadSystem = useCallback(async () => {
    if (!token) return
    try {
      const result = await get<{ project?: Project }>(token, '/api/v1/system')
      if (result.project) setProject(result.project)
    } catch (error) {
      setToast({ kind: 'error', error })
    }
  }, [token])

  const loadWorkspace = useCallback(async (id: string) => {
    if (!token) return
    setWorkspace((current) => ({ ...current, loading: true, error: null }))
    try {
      const [agentResult, recommendationResult, resourceResult] = await Promise.all([
        get<{ agents: Agent[] }>(token, `/api/v1/projects/${id}/agents`),
        get<RecommendationResult>(token, `/api/v1/projects/${id}/recommendations`),
        get<{ resources: Resource[] }>(token, `/api/v1/projects/${id}/resources`),
      ])
      setWorkspace({ data: { agents: agentResult.agents, recommendations: recommendationResult, resources: resourceResult.resources }, loading: false, error: null })
    } catch (error) {
      setWorkspace({ data: null, loading: false, error })
    }
  }, [token])

  useEffect(() => { void loadSystem() }, [loadSystem])
  useEffect(() => {
    if (project && view === 'workspace') void loadWorkspace(project.id)
  }, [project, view, loadWorkspace])

  const runRequest = useCallback(async <T,>(action: () => Promise<T>, onSuccess: (value: T) => void, onError?: (error: unknown) => void) => {
    // Ref ownership is synchronous, so navigation or same-tick events cannot overlap requests.
    if (mutationRequestInFlight.current) return
    mutationRequestInFlight.current = true
    setBusy(true)
    setToast(null)
    try {
      onSuccess(await action())
    } catch (error) {
      if (onError) onError(error)
      else setToast({ kind: 'error', error })
    } finally {
      mutationRequestInFlight.current = false
      setBusy(false)
    }
  }, [])

  const registerProject = async (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!token || !projectPath.trim()) return
    await runRequest(
      () => post<{ project: Project }>(token, '/api/v1/projects/inspect', { path: projectPath.trim() }),
      (result) => {
        setProject(result.project)
        setProjectPath(result.project.path)
        setToast({ kind: 'success', text: 'Registered {project}.', values: { project: result.project.name } })
      },
    )
  }

  const loadSkills = async (searchTerm = query) => {
    if (!token || mutationRequestInFlight.current || skills.loading) return
    setSkills((current) => ({ ...current, loading: true, error: null }))
    setFullCatalog((current) => ({ ...current, loading: true, error: null }))
    invalidateBatchPreview()
    try {
      // Availability always comes from an unfiltered catalog, even during a search.
      const fullRequest = get<InstallationCatalog>(token, '/api/catalog?q=')
      const [result, fullResult] = await Promise.all([
        searchTerm ? get<InstallationCatalog>(token, `/api/catalog?q=${encodeURIComponent(searchTerm)}`) : fullRequest,
        fullRequest,
      ])
      setSkills({ data: result, loading: false, error: null })
      setFullCatalog({ data: fullResult, loading: false, error: null })
    } catch (error) {
      setSkills({ data: null, loading: false, error })
      setFullCatalog({ data: null, loading: false, error })
    }
  }

  const loadRecommendations = useCallback(async (id: string) => {
    if (!token) return
    setRecommendations((current) => ({ ...current, loading: true, error: null }))
    try {
      const result = await get<RecommendationResult>(token, `/api/v1/projects/${id}/recommendations`)
      setRecommendations({ data: result, loading: false, error: null })
    } catch (error) {
      setRecommendations({ data: null, loading: false, error })
    }
  }, [token])

  useEffect(() => {
    if (project && view === 'skills') void loadRecommendations(project.id)
  }, [project, view, loadRecommendations])

  const openSkill = async (skill: Skill) => {
    setSelectedSkill(skill)
    if (!token) return
    try {
      const result = await get<{ skill: Skill }>(token, `/api/v1/skills/${encodeURIComponent(skill.identifier)}`)
      setSelectedSkill(result.skill)
    } catch (error) {
      setToast({ kind: 'error', error })
    }
  }

  const openWorkspaceSkill = (skill: Skill) => {
    setView('skills')
    void openSkill(skill)
  }

  const loadSubAgents = async () => {
    if (!token) return
    setSubagents((current) => ({ ...current, loading: true, error: null }))
    try {
      const result = await get<{ definitions: SubAgent[]; diagnostics: SubAgentDiagnostic[] }>(token, '/api/v1/subagents')
      setSubagents({ data: result, loading: false, error: null })
    } catch (error) {
      setSubagents({ data: null, loading: false, error })
    }
  }

  const loadDiagnostics = async () => {
    if (!token || !projectBase) return
    setFindings((current) => ({ ...current, loading: true, error: null }))
    try {
      const result = await get<{ findings: Finding[] }>(token, `${projectBase}/diagnostics`)
      setFindings({ data: result.findings, loading: false, error: null })
    } catch (error) {
      setFindings({ data: null, loading: false, error })
    }
  }

  const loadLatest = async () => {
    if (!token || !projectBase) return
    setLatest((current) => ({ ...current, loading: true, error: null }))
    try {
      const result = await get<{ latest: LatestOperation }>(token, `${projectBase}/operations/latest`)
      setLatest({ data: result.latest, loading: false, error: null })
    } catch (error) {
      setLatest({ data: null, loading: false, error })
    }
  }

  useEffect(() => {
    if (view === 'skills' && !skills.data && !skills.loading) void loadSkills('')
    if (view === 'subagents' && !subagents.data && !subagents.loading) void loadSubAgents()
    if (view === 'diagnostics' && !findings.data && !findings.loading) void loadDiagnostics()
    if (view === 'operations' && !latest.data && !latest.loading) void loadLatest()
    // A view load is intentionally keyed to navigation; query changes submit through the form.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, project])

  const createActivationPlan = async () => {
    if (!token || !project || mutationBlocked || pendingPlan || selectionAction !== 'activate' || !targets.length) return
    await runRequest(
      () => post<PlanEnvelope>(token, `${projectBase}/plans/activate`, {
        skillIdentifiers: selectedSkillIDs,
        targetAgents: targets,
        ...(forceReplace ? { conflictStrategy: 'replace', forceConfirmed: true } : {}),
      }),
      (result) => { setPendingPlan(result.plan); setPendingActivation(true) },
    )
  }

  const batchPayload = () => ({ skillIds: selectedSkillIDs, targets: batchTargets, replaceConflicts: batchReplaceConflicts })

  const createBatchPreview = async () => {
    if (!token || mutationRequestInFlight.current || mutationBlocked || pendingPlan || selectionAction !== 'batch-install' || !batchTargetsValid) return
    invalidateBatchPreview()
    const inputGeneration = batchInputGeneration.current
    await runRequest(
      () => requestInstallationPreview(token, batchPayload()),
      (result) => {
        if (inputGeneration !== batchInputGeneration.current) return
        setBatchPreview({ ...result.preview, inputGeneration })
        if (result.error) setToast({ kind: 'error', error: result.error })
      },
    )
  }

  const applyBatchPreview = async () => {
    if (!token || !batchPreview || !batchPreview.id.trim() || mutationRequestInFlight.current || pendingPlan || mutationBlocked || selectionAction !== 'batch-install' || !batchTargetsValid || batchPreview.inputGeneration !== batchInputGeneration.current) return
    const inputGeneration = batchPreview.inputGeneration
    await runRequest(
      async () => {
        try {
          const result = await post<InstallationApplyResult>(token, '/api/install/apply', { previewId: batchPreview.id })
          if (!result.result.applied) throw new Error('Installation could not be completed.')
          return { stale: false as const }
        } catch (error) {
          setBatchPreview(null)
          if (!(error instanceof APIError) || error.status !== 409 || inputGeneration !== batchInputGeneration.current) throw error
          const result = await requestInstallationPreview(token, batchPayload())
          return { stale: true as const, ...result }
        }
      },
      (result) => {
        if (result.stale) {
          if (inputGeneration !== batchInputGeneration.current) return
          setBatchPreview({ ...result.preview, inputGeneration })
          if (result.error) setToast({ kind: 'error', error: result.error })
          else setToast({ kind: 'success', text: 'The project changed after preview. Review this updated plan, then confirm again.' })
          return
        }
        invalidateBatchPreview()
        setSelectedSkillIDs([])
        setToast({ kind: 'success', text: 'Selected Skills installed successfully.' })
        if (project) void loadWorkspace(project.id)
        void loadLatest()
      },
    )
  }

  const createRemovalPlan = async (resource: Resource) => {
    if (!token || !project) return
    await runRequest(
      () => post<PlanEnvelope>(token, `${projectBase}/plans/remove`, { skillIdentifier: resource.identifier, targetAgent: resource.target }),
      (result) => { setPendingPlan(result.plan); setPendingActivation(false); invalidateBatchPreview() },
    )
  }

  const createUndoPlan = async () => {
    if (!token || !project) return
    await runRequest(
      () => post<PlanEnvelope>(token, `${projectBase}/plans/undo`, {}),
      (result) => { setPendingPlan(result.plan); setPendingActivation(false); invalidateBatchPreview() },
    )
  }

  const createDiagnosticRepairPlan = async (finding: Finding) => {
    if (!token || !project || !finding.repairAction) return
    const route = finding.repairAction === 'reconcile' ? 'doctor-reconcile' : 'doctor-gitignore'
    await runRequest(
      () => post<PlanEnvelope>(token, `${projectBase}/plans/${route}`, { path: finding.path }),
      (result) => { setPendingPlan(result.plan); setPendingActivation(false); invalidateBatchPreview() },
    )
  }

  const executePlan = async () => {
    if (!token || !pendingPlan || (pendingActivation && (mutationBlocked || selectionAction !== 'activate'))) return
    await runRequest(
      () => post<{ operation: string; status: string }>(token, `/api/v1/plans/${pendingPlan.id}/execute`, {}),
      (result) => {
        setPendingPlan(null)
        if (pendingActivation) setSelectedSkillIDs([])
        setPendingActivation(false)
        setToast({ kind: 'success', text: '{operation} completed and journaled.', values: { operation: operationLabel(result.operation, translateMessage) } })
        if (project) void loadWorkspace(project.id)
        void loadDiagnostics()
        void loadLatest()
      },
      (error) => {
        setPendingPlan(null)
        setPendingActivation(false)
        setToast({ kind: 'error', error })
        if (project) void loadWorkspace(project.id)
        void loadDiagnostics()
        void loadLatest()
      },
    )
  }

  const cancelPlan = async () => {
    if (!token || !pendingPlan) return
    await runRequest(
      () => post<{ status: string }>(token, `/api/v1/plans/${pendingPlan.id}/cancel`, {}),
      () => {
        setPendingPlan(null)
        setPendingActivation(false)
        setToast({ kind: 'success', text: 'The pending plan was cancelled.' })
      },
    )
  }

  const navigate = (next: View) => {
    if (next !== 'skills') invalidateBatchPreview()
    setView(next)
    setToast(null)
    setSelectedSkill(null)
  }

  const pageTitle = useMemo(() => t(navItems.find((item) => item.id === view)?.label ?? 'Workspace'), [t, view])

  if (!token) {
    return <main className={styles.sessionPage}><div className={styles.sessionCard}>
      <span className={styles.kicker}>{t('LOCAL SESSION')}</span>
      <LanguageSwitch />
      <h1>{t('Open the console entry link')}</h1>
      <p>{t('The session key is kept in this browser tab. Start ')}<code>agent-manager web</code>{t(' and open the URL it prints.')}</p>
    </div></main>
  }

  return <div className={styles.appShell}>
    <header className={styles.mobileHeader}>
      <div className={styles.brandMark} aria-hidden="true">A<span>.</span></div>
      <div><p className={styles.brandTitle}>{t('Agent Manager')}</p><p className={styles.brandMeta}>{t('LOCAL CONSOLE')}</p></div>
      <LanguageSwitch />
      <span className={styles.localBadge}><i /> {t('Local')}</span>
    </header>
    <aside className={styles.sidebar} aria-label={t('Main navigation')}>
      <div className={styles.brandBlock}>
        <div className={styles.brandMark} aria-hidden="true">A<span>.</span></div>
        <div><p className={styles.brandTitle}>{t('Agent Manager')}</p><p className={styles.brandMeta}>{t('LOCAL CONSOLE')}</p></div>
      </div>
      <div className={styles.navLabel}>{t('PROJECT DESK')}</div>
      <nav className={styles.navList}>
        {navItems.map((item) => <button key={item.id} type="button" className={`${styles.navItem} ${view === item.id ? styles.navItemActive : ''}`} onClick={() => navigate(item.id)} aria-current={view === item.id ? 'page' : undefined}>
          <span className={styles.navIndex}>{item.eyebrow}</span><span>{t(item.label)}</span>
        </button>)}
      </nav>
      <div className={styles.sidebarFoot}>
        <span className={styles.localBadge}><i /> {t('Local only')}</span>
        <p>{t('Project changes are reviewed before they are applied.')}</p>
        <LanguageSwitch />
      </div>
    </aside>

    <main className={styles.mainArea}>
      <div className={styles.topline}>
        <span>{project?.name ?? t('No project registered')}</span>
        <span className={styles.pathText}>{project?.path ?? t('Choose a local directory to begin')}</span>
        <span className={styles.localBadge}><i /> {t('Session active')}</span>
      </div>
      <div className={styles.pageHeading}>
        <div><span className={styles.kicker}>AGENT MANAGER / {String(navItems.findIndex((item) => item.id === view) + 1).padStart(2, '0')}</span><h1>{pageTitle}</h1></div>
        {project && view === 'workspace' && <button className={styles.secondaryButton} type="button" onClick={() => void loadWorkspace(project.id)} disabled={workspace.loading}>↻ <span>{t('Refresh')}</span></button>}
      </div>

      {toast && <div className={`${styles.notice} ${toast.kind === 'error' ? styles.noticeError : styles.noticeSuccess}`} role={toast.kind === 'error' ? 'alert' : 'status'}>
        {toast.kind === 'error' ? formatError(toast.error) : t(toast.text, toast.values)}
        <button type="button" aria-label={t('Dismiss message')} onClick={() => setToast(null)}>×</button>
      </div>}

      {!project ? (view === 'sync'
        ? <StorageSyncPage token={token} hasProject={false} />
        : <ProjectRegistration path={projectPath} setPath={setProjectPath} onSubmit={registerProject} busy={busy} />) : <>
        {pendingPlan && <PlanReview plan={pendingPlan} busy={busy} canExecute={!pendingActivation || !mutationBlocked} onExecute={() => void executePlan()} onCancel={() => void cancelPlan()} />}
        {view === 'workspace' && <Workspace
          page={workspace}
          busy={busy}
          onActivate={openWorkspaceSkill}
          onRemove={(resource) => void createRemovalPlan(resource)}
          onDiagnostics={() => navigate('diagnostics')}
        />}
        {view === 'skills' && <SkillsPage
          page={skills}
          recommendations={recommendations}
          selectedSkillIDs={selectedSkillIDs}
          selectionAction={selectionAction}
          unavailableSkillIDs={unavailableSkillIDs}
          mutationBlocked={mutationBlocked || !!pendingPlan}
          inputsLocked={busy || pendingActivation}
          onRetryRecommendations={() => void loadRecommendations(project.id)}
          onToggleSkill={(identifier) => { invalidateBatchPreview(); setSelectedSkillIDs((current) => toggleSkillSelection(current, identifier)) }}
          query={query}
          setQuery={setQuery}
          onSearch={(event) => { event.preventDefault(); void loadSkills(query) }}
          selected={selectedSkill}
          onSelect={(skill) => { if (skill) void openSkill(skill); else setSelectedSkill(null) }}
          targets={targets}
          onToggleTarget={(target) => setTargets((current) => current.includes(target) ? current.filter((item) => item !== target) : [...current, target])}
          supportedTargets={fullCatalog.data?.targets ?? []}
          batchTargets={batchTargets}
          onToggleBatchTarget={(target) => { invalidateBatchPreview(); setBatchTargets((current) => current.includes(target) ? current.filter((item) => item !== target) : [...current, target]) }}
          batchReplaceConflicts={batchReplaceConflicts}
          setBatchReplaceConflicts={(value) => { invalidateBatchPreview(); setBatchReplaceConflicts(value) }}
          batchTargetsValid={batchTargetsValid}
          batchPreview={batchPreview}
          onPreviewInstallation={() => void createBatchPreview()}
          onApplyInstallation={() => void applyBatchPreview()}
          onCancelInstallation={invalidateBatchPreview}
          forceReplace={forceReplace}
          setForceReplace={setForceReplace}
          onActivate={() => void createActivationPlan()}
          busy={busy}
        />}
        {view === 'subagents' && <SubAgentsPage page={subagents} />}
        {view === 'diagnostics' && <DiagnosticsPage
          page={findings}
          scanDiagnostics={workspace.data?.recommendations.diagnostics ?? []}
          onRefresh={() => { void loadDiagnostics(); if (project) void loadWorkspace(project.id) }}
          onRepair={(finding) => void createDiagnosticRepairPlan(finding)}
          busy={busy}
        />}
        {view === 'operations' && <OperationsPage page={latest} onUndo={() => void createUndoPlan()} busy={busy} />}
        {view === 'sync' && <StorageSyncPage token={token} hasProject={Boolean(project)} />}
      </>}
      <footer className={styles.pageFooter}><span>AGENT MANAGER</span><span>{t('LOCAL SESSION · NO NETWORK CONTENT')}</span></footer>
    </main>
  </div>
}

function LanguageSwitch() {
  const { locale, setLocale, t } = useLanguage()
  return <div className={styles.languageSwitch} role="group" aria-label={t('Select console language')}>
    <button type="button" aria-label={t('English')} aria-pressed={locale === 'en'} onClick={() => setLocale('en')}>EN</button>
    <button type="button" aria-label={t('Simplified Chinese')} aria-pressed={locale === 'zh-CN'} onClick={() => setLocale('zh-CN')}>中文</button>
  </div>
}

type SyncOperationReview =
  | { kind: 'resync'; planId: string; scope: 'global' | 'project'; plan: SyncPlan['plan'] }
  | { kind: 'resync-undo'; planId: string; scope: 'global' | 'project'; plan: SyncPlan['plan'] }
type UpstreamUpdateReview =
  | { kind: 'update'; planId: string; preview: SkillUpdatePreview }
  | { kind: 'undo'; planId: string; plan: SyncPlan['plan'] }

function StorageSyncPage({ token, hasProject }: { token: string; hasProject: boolean }) {
  const { t, error: formatError } = useLanguage()
  const [inventory, setInventory] = useState<SyncInventory | null>(null)
  const [syncMode, setSyncMode] = useState<SyncMode>('auto')
  const [selectedSyncMode, setSelectedSyncMode] = useState<SyncMode>('auto')
  const [selected, setSelected] = useState<string[]>([])
  const [currentChoices, setCurrentChoices] = useState<Record<string, string>>({})
  const [importReview, setImportReview] = useState<{ planId: string; preview: SyncImportPreview } | null>(null)
  const [operationReview, setOperationReview] = useState<SyncOperationReview | null>(null)
  const [updateCheck, setUpdateCheck] = useState<SkillUpdateCheck | null>(null)
  const [selectedUpdates, setSelectedUpdates] = useState<string[]>([])
  const [upstreamReview, setUpstreamReview] = useState<UpstreamUpdateReview | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [previewingImport, setPreviewingImport] = useState(false)
  const [error, setError] = useState<unknown | null>(null)
  const [notice, setNotice] = useState('')

  const refresh = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [settings, sources] = await Promise.all([
        get<{ repositoryPath: string; syncMode: SyncMode }>(token, '/api/v1/sync/settings'),
        get<SyncInventory>(token, '/api/v1/sync/sources'),
      ])
      setSyncMode(settings.syncMode)
      setSelectedSyncMode(settings.syncMode)
      setInventory(sources)
    } catch (caught) {
      setError(caught)
    } finally {
      setLoading(false)
    }
  }, [token])

  useEffect(() => { void refresh() }, [refresh])

  const perform = async <T,>(action: () => Promise<T>, onSuccess: (result: T) => void) => {
    setBusy(true)
    setError(null)
    setNotice('')
    try { onSuccess(await action()) } catch (caught) { setError(caught) } finally { setBusy(false) }
  }

  const saveMode = async () => {
    const mode = selectedSyncMode
    await perform(
      () => post<{ syncMode: SyncMode }>(token, '/api/v1/sync/settings', { syncMode: mode }, 'PUT'),
      (result) => { setSyncMode(result.syncMode); setNotice(t('Sync mode saved. Resync a scope to update existing activations.')) },
    )
  }

  const createImportPreview = async () => {
    setPreviewingImport(true)
    try {
      await perform(
        () => post<{ planId: string; preview: SyncImportPreview }>(token, '/api/v1/sync/import/preview', { selectedGroupIds: selected, currentChoices }),
        (result) => setImportReview(result),
      )
    } finally {
      setPreviewingImport(false)
    }
  }

  const applyImport = async () => {
    if (!importReview) return
    await perform(
      () => post<{ imported: number }>(token, '/api/v1/sync/import/apply', { planId: importReview.planId }),
      (result) => {
        setNotice(t('{count} Skill versions imported. Sources were left in place.', { count: result.imported }))
        setImportReview(null)
        setSelected([])
        setCurrentChoices({})
        void refresh()
      },
    )
  }

  const previewResync = async (scope: 'global' | 'project') => {
    await perform(
      () => post<{ planId: string; scope: string; plan: SyncPlan['plan'] }>(token, '/api/v1/sync/resync/preview', { scope }),
      (result) => setOperationReview({ kind: 'resync', planId: result.planId, scope, plan: result.plan }),
    )
  }

  const previewResyncUndo = async (scope: 'global' | 'project') => {
    await perform(
      () => post<{ planId: string; plan: SyncPlan['plan'] }>(token, '/api/v1/sync/resync/undo/preview', { scope }),
      (result) => setOperationReview({ kind: 'resync-undo', planId: result.planId, scope, plan: result.plan }),
    )
  }

  const checkUpdates = async () => {
    await perform(
      () => post<SkillUpdateCheck>(token, '/api/v1/sync/updates/check'),
      (result) => { setUpdateCheck(result); setSelectedUpdates([]) },
    )
  }

  const previewUpdates = async (ids: string[]) => {
    if (ids.length === 0) return
    await perform(
      () => post<{ planId: string; preview: SkillUpdatePreview }>(token, '/api/v1/sync/updates/preview', { selectedSourceIds: ids }),
      (result) => setUpstreamReview({ kind: 'update', ...result }),
    )
  }

  const previewUpdatesUndo = async () => {
    await perform(
      () => post<{ planId: string; plan: SyncPlan['plan'] }>(token, '/api/v1/sync/updates/undo/preview'),
      (result) => setUpstreamReview({ kind: 'undo', ...result }),
    )
  }

  const applyUpstreamReview = async () => {
    if (!upstreamReview) return
    const { kind, planId } = upstreamReview
    await perform(
      () => post<{ updated?: number; status?: string }>(token, kind === 'update' ? '/api/v1/sync/updates/apply' : '/api/v1/sync/updates/undo/apply', { planId }),
      (result) => {
        setNotice(kind === 'update' ? t('{count} Skills updated from their recorded sources.', { count: result.updated ?? 0 }) : t('The latest upstream Skill update was undone.'))
        setUpstreamReview(null)
        setUpdateCheck(null)
        setSelectedUpdates([])
        void refresh()
      },
    )
  }

  const applyOperationReview = async () => {
    if (!operationReview) return
    const { kind, planId, scope } = operationReview
    const route = kind === 'resync' ? '/api/v1/sync/resync/apply' : '/api/v1/sync/resync/undo/apply'
    await perform(
      () => post<{ operation?: string; status?: string }>(token, route, { planId }),
      () => {
        setNotice(kind === 'resync' ? t('{scope} activations resynchronized.', { scope: scopeLabel(scope, t) }) : t('{scope} resync was undone.', { scope: scopeLabel(scope, t) }))
        setOperationReview(null)
        void refresh()
      },
    )
  }

  const toggleGroup = (groupID: string) => setSelected((current) => current.includes(groupID) ? current.filter((id) => id !== groupID) : [...current, groupID])
  const allGroups = inventory?.groups ?? []
  const currentPreviewNeedsChoice = importReview?.preview.currentChoiceRequired ?? []
  const currentPreviewChoices = Array.from(new Set(importReview?.preview.groups.filter((group) => group.requiresCurrentChoice).map((group) => group.identifier) ?? []))
  const hasUnappliedCurrentChoice = Boolean(importReview && currentPreviewChoices.some((identifier) => currentChoices[identifier] !== importReview.preview.currentChoices?.[identifier]))
  const pendingCurrentChoices = Array.from(new Set([
    ...currentPreviewNeedsChoice,
    ...currentPreviewChoices.filter((identifier) => currentChoices[identifier] && currentChoices[identifier] !== importReview?.preview.currentChoices?.[identifier]),
  ]))
  const conflictingIdentifiers = Array.from(new Set(importReview?.preview.groups.filter((group) => group.conflict).map((group) => group.identifier) ?? []))
  const hasImportableVersion = Boolean(importReview?.preview.groups.some((group) => !group.alreadyPresent))
  const canApplyImport = Boolean(importReview && currentPreviewNeedsChoice.length === 0 && !hasUnappliedCurrentChoice && importReview.preview.groups.some((group) => !group.alreadyPresent) && importReview.preview.groups.every((group) => !group.conflict))
  const availableUpdates = updateCheck?.items.filter((item) => item.canUpdate) ?? []
  const reviewActive = Boolean(importReview || operationReview || upstreamReview)

  if (loading && !inventory) return <LoadingState label={t('Reading supported Skill locations…')} />
  if (error && !inventory) return <ErrorState error={error} />
  if (!inventory) return <EmptyState title={t('Storage & Sync is unavailable')} detail={t('The local synchronization inventory could not be loaded.')} />

  return <div className={styles.syncPage}>
    {notice && <div className={styles.syncNotice} role="status">{notice}<button type="button" onClick={() => setNotice('')} aria-label={t('Dismiss message')}>×</button></div>}
    {error !== null && !importReview && <div className={styles.syncError} role="alert">{formatError(error)}</div>}

    <section className={styles.panel} aria-labelledby="sync-library-heading">
      <div className={styles.panelTop}><span className={styles.kicker}>{t('CENTRAL SKILL LIBRARY')}</span><span className={styles.statusDot}><i /> {t('Local only')}</span></div>
      <h2 id="sync-library-heading">{t('Storage & Sync')}</h2>
      <p>{t('Imported Skills are copied into the Agent Manager repository. Their source folders stay unchanged, and importing does not activate them.')}</p>
      <div className={styles.syncRepoPath}><span>{t('Repository path')}</span><code>{inventory.repositoryPath}</code></div>
      <fieldset className={styles.syncModes}>
        <legend>{t('How new activations are synchronized')}</legend>
        {(['auto', 'symlink-only', 'copy-only'] as SyncMode[]).map((mode) => <label key={mode}>
          <input type="radio" name="sync-mode" value={mode} checked={selectedSyncMode === mode} disabled={busy || reviewActive} onChange={() => setSelectedSyncMode(mode)} />
          <span><strong>{modeLabel(mode, t)}</strong><small>{modeDescription(mode, t)}</small></span>
        </label>)}
      </fieldset>
      <div className={styles.syncActions}><button className={styles.primaryButton} type="button" disabled={busy || reviewActive || selectedSyncMode === syncMode} onClick={() => void saveMode()}>{t('Save sync mode')}</button></div>
    </section>

    <section className={styles.panel} aria-labelledby="source-inventory-heading">
      <div className={styles.panelTop}><span className={styles.kicker}>{t('SOURCE INVENTORY')}</span><button className={styles.textButton} type="button" disabled={loading || busy || reviewActive} onClick={() => void refresh()}>{t('Refresh sources')}</button></div>
      <h2 id="source-inventory-heading">{t('Skills ready to import')}</h2>
      <p>{t('Choose a version to add it to the central library. Matching content from several locations is grouped once.')}</p>
      <div className={styles.syncScopes}>
        <SyncSourceSection scope="global" groups={allGroups} sources={inventory.sources} selected={selected} disabled={busy || reviewActive} onToggle={toggleGroup} onSelectAll={(ids, checked) => setSelected((current) => checked ? Array.from(new Set([...current, ...ids])) : current.filter((id) => !ids.includes(id)))} />
        <SyncSourceSection scope="project" groups={allGroups} sources={inventory.sources} selected={selected} disabled={busy || reviewActive || !hasProject} onToggle={toggleGroup} onSelectAll={(ids, checked) => setSelected((current) => checked ? Array.from(new Set([...current, ...ids])) : current.filter((id) => !ids.includes(id)))} />
      </div>
      {inventory.diagnostics.length > 0 && <div className={styles.syncDiagnostics}>
        <strong>{t('{count} scan notes', { count: inventory.diagnostics.length })}</strong>
        {inventory.diagnostics.map((item, index) => <div key={`${item.path}-${index}`}><code>{item.scope === 'global' ? t('Global') : t('Project')} · {item.path}</code><span>{item.message}</span></div>)}
      </div>}
      <div className={styles.syncActions}>
        <button className={styles.primaryButton} type="button" disabled={busy || reviewActive || selected.length === 0} onClick={() => void createImportPreview()}>{previewingImport ? t('Creating import preview…') : t('Review selected import')}</button>
      </div>
    </section>

    <section className={styles.panel} aria-labelledby="upstream-updates-heading">
      <div className={styles.panelTop}><span className={styles.kicker}>{t('RECORDED UPSTREAM SOURCES')}</span>
        <button className={styles.textButton} type="button" disabled={busy || reviewActive} onClick={() => void checkUpdates()}>{busy ? t('Checking…') : t('Check for updates')}</button>
      </div>
      <h2 id="upstream-updates-heading">{t('Check installed Skill updates')}</h2>
      <p>{t('Only this button contacts supported GitHub sources recorded by Skills CLI. Local and unsupported sources are shown without a network check.')}</p>
      {updateCheck && <>
        <div className={styles.syncMuted}>{t('Last checked: {time}', { time: new Date(updateCheck.checkedAt).toLocaleString() })}</div>
        {updateCheck.items.length === 0 ? <EmptyInline title={t('No Skills with recorded sources')} detail={t('Refresh the source inventory after installing a Skill with Skills CLI.')} /> : <div className={styles.updateList}>
          {updateCheck.items.map((item) => <SkillUpdateRow key={item.id} item={item} checked={selectedUpdates.includes(item.id)} disabled={busy || reviewActive} onToggle={() => setSelectedUpdates((current) => current.includes(item.id) ? current.filter((id) => id !== item.id) : [...current, item.id])} />)}
        </div>}
        <div className={styles.syncActions}>
          <button className={styles.primaryButton} type="button" disabled={busy || reviewActive || availableUpdates.length === 0} onClick={() => void previewUpdates(availableUpdates.map((item) => item.id))}>{t('Update all available')}</button>
          <button className={styles.secondaryButton} type="button" disabled={busy || reviewActive || selectedUpdates.length === 0} onClick={() => void previewUpdates(selectedUpdates)}>{t('Update selected')}</button>
        </div>
      </>}
      {inventory.updateUndoAvailable && <div className={styles.syncActions}>
        <button className={styles.textButton} type="button" disabled={busy || reviewActive} onClick={() => void previewUpdatesUndo()}>{t('Undo latest Skill update')}</button>
      </div>}
    </section>

    <section className={styles.panel} aria-labelledby="resync-heading">
      <div className={styles.panelTop}><span className={styles.kicker}>{t('EXISTING ACTIVATIONS')}</span><span className={styles.countMark}>{t('Scope by scope')}</span></div>
      <h2 id="resync-heading">{t('Resync managed Skills')}</h2>
      <p>{t('Apply the saved synchronization mode to journal-managed activations. Each scope has its own review, operation journal, and Undo.')}</p>
      <div className={styles.syncActions}>
        <button className={styles.secondaryButton} type="button" disabled={busy || reviewActive} onClick={() => void previewResync('global')}>{t('Review global resync')}</button>
        <button className={styles.secondaryButton} type="button" disabled={busy || reviewActive || !hasProject} onClick={() => void previewResync('project')}>{t('Review project resync')}</button>
        <button className={styles.textButton} type="button" disabled={busy || reviewActive || !inventory.globalResyncUndoAvailable} onClick={() => void previewResyncUndo('global')}>{t('Global Undo')}</button>
        <button className={styles.textButton} type="button" disabled={busy || reviewActive || !hasProject || !inventory.projectResyncUndoAvailable} onClick={() => void previewResyncUndo('project')}>{t('Project Undo')}</button>
      </div>
    </section>

    {importReview && <section className={styles.syncReview} aria-labelledby="import-review-heading">
      <div className={styles.panelTop}><span className={styles.kicker}>{t('IMPORT PREVIEW')}</span><span className={styles.pill}>{t('{count} selected versions', { count: importReview.preview.groups.length })}</span></div>
      <h2 id="import-review-heading">{t('Review before importing')}</h2>
      {error !== null && <div className={styles.syncError} role="alert">{formatError(error)}</div>}
      {previewingImport && <p className={styles.syncReviewStatus} role="status">{t('Updating import preview…')}</p>}
      <p className={styles.syncMuted}>{t('Local timestamps are comparison hints only; choose the version you want to keep.')}</p>
      {importReview.preview.groups.map((group) => <SyncGroupReview key={group.id} group={group} t={t} />)}
      {currentPreviewChoices.map((identifier) => <fieldset className={styles.currentChoice} key={identifier}>
        <legend>{t('Choose the current version for {identifier}', { identifier })}</legend>
        {importReview.preview.groups.filter((group) => group.identifier === identifier).map((group) => <label key={group.id}>
          <input type="radio" name={`current-${identifier}`} checked={currentChoices[identifier] === group.id} disabled={busy} onChange={() => setCurrentChoices((current) => ({ ...current, [identifier]: group.id }))} />
          <span>
            {group.sources.map((source) => <small key={source.id}>{source.location} · {source.scope === 'global' ? t('Global') : t('Project')} · {source.modifiedAt ? t('Local modified: {time}', { time: new Date(source.modifiedAt).toLocaleString() }) : t('Local modification time unavailable')}</small>)}
            {group.newerLocalModification && <small><strong className={`${styles.pill} ${styles.pillReady}`}>{t('Local modification time is newer')}</strong></small>}
            {group.sources.length > 1 && group.modifiedAt && <small>{t('Newest local modification: {time}', { time: new Date(group.modifiedAt).toLocaleString() })}</small>}
            <code>{t('Content checksum: {checksum}', { checksum: group.checksum.slice(0, 12) })}</code>
            {currentChoices[identifier] === group.id && <strong className={`${styles.pill} ${styles.pillReady}`}>{currentChoices[identifier] === importReview.preview.currentChoices?.[identifier] ? t('Current-version choice is included in preview') : t('Selected for current version; preview not updated')}</strong>}
          </span>
        </label>)}
        {!currentChoices[identifier] && <small>{t('Choose a candidate, then update the preview before importing.')}</small>}
        {currentChoices[identifier] && currentChoices[identifier] === importReview.preview.currentChoices?.[identifier] && <small>{t('This choice is already included in the preview.')}</small>}
        <button className={styles.secondaryButton} type="button" disabled={busy || !currentChoices[identifier] || currentChoices[identifier] === importReview.preview.currentChoices?.[identifier]} onClick={() => void createImportPreview()}>{previewingImport ? t('Updating preview…') : t('Update preview with this choice')}</button>
      </fieldset>)}
      {importReview.preview.groups.some((group) => group.alreadyPresent) && <p className={styles.syncMuted}>{t('Versions already in the repository will be skipped.')}</p>}
      {pendingCurrentChoices.length > 0 && <p className={styles.syncReviewStatus} data-state="blocked" role="status">{t('Complete the current-version choices for {skills}: select each candidate and update the preview before confirming.', { skills: pendingCurrentChoices.join(', ') })}</p>}
      {pendingCurrentChoices.length === 0 && conflictingIdentifiers.length > 0 && <p className={styles.syncReviewStatus} data-state="blocked" role="alert">{t('Resolve the repository conflicts for {skills} before importing.', { skills: conflictingIdentifiers.join(', ') })}</p>}
      {pendingCurrentChoices.length === 0 && conflictingIdentifiers.length === 0 && !hasImportableVersion && <p className={styles.syncReviewStatus} data-state="blocked" role="status">{t('All selected versions are already in the repository; there is nothing to import.')}</p>}
      {canApplyImport && <p className={styles.syncReviewStatus} data-state="ready" role="status">{t('The preview is current and ready for confirmation.')}</p>}
      <div className={styles.syncActions}>
        <button className={styles.primaryButton} type="button" disabled={busy || !canApplyImport} onClick={() => void applyImport()}>{busy && !previewingImport ? t('Importing…') : t('Confirm import')}</button>
        <button className={styles.secondaryButton} type="button" disabled={busy} onClick={() => setImportReview(null)}>{t('Cancel')}</button>
      </div>
    </section>}

    {upstreamReview && <section className={styles.syncReview} aria-labelledby="upstream-review-heading">
      <div className={styles.panelTop}><span className={styles.kicker}>{upstreamReview.kind === 'update' ? t('UPSTREAM UPDATE PREVIEW') : t('UPDATE UNDO')}</span><span className={styles.pill}>{upstreamReview.kind === 'update' ? t('{count} Skills', { count: upstreamReview.preview.items.length }) : t('Reviewed restoration')}</span></div>
      <h2 id="upstream-review-heading">{upstreamReview.kind === 'update' ? t('Review Skill updates') : t('Review the latest upstream update Undo')}</h2>
      {upstreamReview.kind === 'update' ? upstreamReview.preview.items.map((item) => <div className={styles.syncPlanRow} key={item.id}>
        <strong>{item.identifier} · {item.scope === 'global' ? t('Global') : t('Project')}</strong>
        <small>{t('Source: {source}', { source: item.origin })} · {t('Ref: {ref}', { ref: item.ref })}</small>
        <code>{item.path}</code>
        <small>{t('Current hash: {hash}', { hash: item.currentHash })}</small>
        <small>{t('New hash: {hash}', { hash: item.upstreamHash })}</small>
      </div>) : upstreamReview.plan.changes.map((change, index) => <div className={styles.syncPlanRow} key={`${change.path}-${index}`}>
        <strong>{change.action}</strong><code>{change.path}</code>{change.detail && <small>{change.detail}</small>}
      </div>)}
      <p className={styles.syncMuted}>{upstreamReview.kind === 'update' ? t('Review each source path and hash. Files change only after you confirm.') : t('Undo restores the Skill files and source lock metadata captured before the update.')}</p>
      <div className={styles.syncActions}>
        <button className={upstreamReview.kind === 'update' ? styles.primaryButton : styles.subtleDangerButton} type="button" disabled={busy || (upstreamReview.kind === 'undo' && upstreamReview.plan.changes.length === 0)} onClick={() => void applyUpstreamReview()}>{upstreamReview.kind === 'update' ? t('Confirm Skill updates') : t('Confirm update Undo')}</button>
        <button className={styles.secondaryButton} type="button" disabled={busy} onClick={() => setUpstreamReview(null)}>{t('Cancel')}</button>
      </div>
    </section>}

    {operationReview && <section className={styles.syncReview} aria-labelledby="resync-review-heading">
      <div className={styles.panelTop}><span className={styles.kicker}>{operationReview.kind === 'resync' ? t('RESYNC PREVIEW') : t('OPERATION UNDO')}</span><span className={styles.pill}>{scopeLabel(operationReview.scope, t)}</span></div>
      <h2 id="resync-review-heading">{operationReview.kind === 'resync' ? t('Review managed destinations') : t('Review the latest scope operation')}</h2>
      {operationReview.plan.changes.length ? operationReview.plan.changes.map((change, index) => <div className={styles.syncPlanRow} key={`${change.path}-${index}`}><strong>{change.action}</strong><code>{change.path}</code>{change.detail && <small>{change.detail}</small>}</div>) : <EmptyInline title={t('No managed activations to resync')} detail={t('This scope has no journal-managed Skills.')} />}
      {operationReview.plan.warnings.map((warning) => <p className={styles.syncMuted} key={warning}>{warning}</p>)}
      <div className={styles.syncActions}>
        <button className={operationReview.kind === 'resync' ? styles.primaryButton : styles.subtleDangerButton} type="button" disabled={busy || operationReview.plan.changes.length === 0} onClick={() => void applyOperationReview()}>{operationReview.kind === 'resync' ? t('Confirm resync') : t('Confirm scope Undo')}</button>
        <button className={styles.secondaryButton} type="button" disabled={busy} onClick={() => setOperationReview(null)}>{t('Cancel')}</button>
      </div>
    </section>}
  </div>
}

function SkillUpdateRow({ item, checked, disabled, onToggle }: {
  item: SkillUpdateItem
  checked: boolean
  disabled: boolean
  onToggle: () => void
}) {
  const { t } = useLanguage()
  const status = item.status === 'available' ? 'Update available'
    : item.status === 'up-to-date' ? 'Up to date'
      : item.status === 'modified-locally' ? 'Locally modified'
        : item.status === 'unsupported' ? 'Cannot check'
          : item.status === 'failed' ? 'Check failed' : 'Local source'
  return <article className={styles.updateRow}>
    {item.canUpdate && <input type="checkbox" checked={checked} disabled={disabled} aria-label={t('Select update for {identifier}', { identifier: item.identifier })} onChange={onToggle} />}
    <div className={styles.updateRowMain}>
      <div className={styles.updateRowHeading}><strong>{item.identifier}</strong><span className={`${styles.pill} ${item.canUpdate ? styles.pillReady : styles.pillMuted}`}>{t(status)}</span></div>
      <small>{t('Scope: {scope}', { scope: item.scope === 'global' ? t('Global') : t('Project') })} · {t('Source: {source}', { source: item.origin || t('Unknown source') })}{item.ref ? ` · ${t('Ref: {ref}', { ref: item.ref })}` : ''}</small>
      <code>{item.path}</code>
      {item.currentHash && <small>{t('Current hash: {hash}', { hash: item.currentHash })}</small>}
      {item.upstreamHash && <small>{t('New hash: {hash}', { hash: item.upstreamHash })}</small>}
      {item.problem && <small className={item.status === 'failed' || item.status === 'modified-locally' ? styles.syncProblem : ''}>{item.problem}</small>}
    </div>
  </article>
}

function SyncSourceSection({ scope, groups, sources, selected, disabled, onToggle, onSelectAll }: {
  scope: 'global' | 'project'
  groups: SyncSourceGroup[]
  sources: SyncSource[]
  selected: string[]
  disabled: boolean
  onToggle: (id: string) => void
  onSelectAll: (ids: string[], checked: boolean) => void
}) {
  const { t } = useLanguage()
  const label = scopeLabel(scope, t)
  const scopedGroups = groups.filter((group) => group.sources.some((source) => source.scope === scope))
  const groupIDs = scopedGroups.map((group) => group.id)
  const allSelected = groupIDs.length > 0 && groupIDs.every((id) => selected.includes(id))
  const otherSources = sources.filter((source) => source.scope === scope && (!source.eligible || source.alreadyManaged))
  return <section className={styles.syncScope} aria-label={label}>
    <div className={styles.syncScopeHeading}><div><span className={styles.kicker}>{scope === 'global' ? t('GLOBAL SOURCES') : t('PROJECT SOURCES')}</span><h3>{label}</h3></div><label className={styles.selectAll}><input type="checkbox" checked={allSelected} disabled={disabled || groupIDs.length === 0} onChange={(event) => onSelectAll(groupIDs, event.target.checked)} />{t('Select all')}</label></div>
    {scopedGroups.length === 0 && <EmptyInline title={t('No eligible Skills found')} detail={scope === 'project' ? t('This project has no eligible source Skills.') : t('No eligible global source Skills were found.')} />}
    <div className={styles.syncGroupList}>{scopedGroups.map((group) => <label className={styles.syncSourceRow} key={group.id}>
      <input type="checkbox" checked={selected.includes(group.id)} disabled={disabled} onChange={() => onToggle(group.id)} aria-label={t('Select {identifier}', { identifier: group.identifier })} />
      <span><strong>{group.identifier}</strong>
        {group.sources.filter((source) => source.scope === scope).map((source) => <small key={source.id}>
          {source.location} · {source.path} · {source.originType === 'local' ? t('Local source') : `${source.originType || t('Unknown source')} · ${source.origin || t('Unknown source')}`}
          {source.modifiedAt ? ` · ${t('Local modified: {time}', { time: new Date(source.modifiedAt).toLocaleString() })}` : ` · ${t('Local modification time unavailable')}`}
        </small>)}
        <small>{t('Content checksum: {checksum}', { checksum: group.checksum })}</small>
        {group.newerLocalModification && <small><strong className={`${styles.pill} ${styles.pillReady}`}>{t('Local modification time is newer')}</strong></small>}
        {group.sources.length > 1 && <small>{t('Same content appears in {count} locations', { count: group.sources.length })}</small>}
      </span>
    </label>)}</div>
    {otherSources.map((source) => <div className={styles.syncSourceNote} key={source.id}><strong>{source.identifier}</strong><small>{source.alreadyManaged ? t('Already managed as {method}', { method: source.managedMaterial ?? '' }) : t('Not eligible · {problem}', { problem: source.problem ?? '' })}</small><code>{source.path}</code></div>)}
  </section>
}

function SyncGroupReview({ group, t }: { group: SyncSourceGroup; t: (source: string, values?: Record<string, string | number>) => string }) {
  return <div className={styles.syncPlanRow}>
    <strong>{group.identifier} · {group.alreadyPresent ? t('Already in repository') : group.conflict ? t('Conflict') : t('New version')}</strong>
    <code>{group.versionKey}</code>
    {group.newerLocalModification && <small><strong className={`${styles.pill} ${styles.pillReady}`}>{t('Local modification time is newer')}</strong></small>}
    {group.makeCurrent && <small>{t('Will become the current version')}</small>}
    {group.conflictReason && <small className={styles.syncProblem}>{group.conflictReason}</small>}
    {group.existingCurrent && <small>{t('Current version stays {version}', { version: group.existingCurrent })}</small>}
    {group.sources.map((source) => <small key={source.id}>{source.scope === 'global' ? t('Global') : t('Project')} · {source.location} · {source.path}</small>)}
  </div>
}

function scopeLabel(scope: 'global' | 'project', t: (source: string) => string) { return scope === 'global' ? t('Global') : t('Project') }
function modeLabel(mode: SyncMode, t: (source: string) => string) { return t(mode === 'auto' ? 'Automatic' : mode === 'symlink-only' ? 'Symlink only' : 'Copy only') }
function modeDescription(mode: SyncMode, t: (source: string) => string) {
  return t(mode === 'auto' ? 'Try a symbolic link first, then copy if links are unavailable.' : mode === 'symlink-only' ? 'Use symbolic links. Link errors are shown for review.' : 'Copy Skill files into the agent location.')
}

function ProjectRegistration({ path, setPath, onSubmit, busy }: { path: string; setPath: (path: string) => void; onSubmit: (event: React.FormEvent<HTMLFormElement>) => void; busy: boolean }) {
  const { t } = useLanguage()
  return <section className={styles.registerPanel}>
    <div className={styles.registerIndex}>01 <span>/ {t('PROJECT')}</span></div>
    <div className={styles.registerCopy}><span className={styles.kicker}>{t('START WITH A PROJECT')}</span><h2>{t('Choose one local workspace.')}</h2><p>{t('The console reads this directory locally and only changes project state through reviewed operation plans. It does not expose general file browsing.')}</p></div>
    <form className={styles.registerForm} onSubmit={onSubmit}>
      <label htmlFor="project-path">{t('Project directory')}</label>
      <div className={styles.inputAction}><input id="project-path" value={path} onChange={(event) => setPath(event.target.value)} placeholder="/Users/you/work/my-project" autoComplete="off" required /><button className={styles.primaryButton} type="submit" disabled={busy}>{busy ? t('Checking…') : t('Inspect project')}</button></div>
      <p>{t('Paste an absolute path or a path relative to where the console was started.')}</p>
    </form>
    <div className={styles.registerStamp}><span>01</span><span>{t('SESSION')}<br />{t('BOUND')}</span></div>
  </section>
}

function Workspace({ page, busy, onActivate, onRemove, onDiagnostics }: {
  page: PageState<{ agents: Agent[]; recommendations: RecommendationResult; resources: Resource[] }>
  onActivate: (skill: Skill) => void
  onRemove: (resource: Resource) => void
  onDiagnostics: () => void
  busy: boolean
}) {
  const { t, message } = useLanguage()
  if (page.loading && !page.data) return <LoadingState label={t('Reading this project…')} />
  if (page.error) return <ErrorState error={page.error} />
  if (!page.data) return <EmptyState title={t('Workspace is ready')} detail={t('Refresh to read the project inventory.')} />
  const { agents, recommendations, resources } = page.data
  const hiddenAgentIDs = new Set(['codebuddy', 'junie', 'lingma', 'vscode'])
  const visibleAgents = agents.filter((agent) => !hiddenAgentIDs.has(agent.id))
  const recommended = recommendations.scopes.flatMap((scope) => scope.recommendations.map((item) => ({ ...item, scope: scope.path })))
  const technologies = recommendations.scopes.flatMap((scope) => scope.technologies.map((technology) => ({
    ...technology,
    scope: scope.path,
    evidence: scope.evidence.filter((item) => item.technology === technology.id).map((item) => item.path),
  })))
  const unavailable = visibleAgents.filter((agent) => agent.availability === 'unavailable' || agent.status === 'unsupported').length
  return <div className={styles.workspaceGrid}>
    <section className={`${styles.panel} ${styles.summaryPanel}`}>
      <div className={styles.panelTop}><span className={styles.kicker}>{t('PROJECT SNAPSHOT')}</span><span className={styles.statusDot}><i /> {t('Read only')}</span></div>
      <h2>{technologies.length ? t('{count} signals found', { count: technologies.length }) : t('No stack signals yet')}</h2>
      <p>{recommendations.scanComplete ? t('Static markers were read from the project tree.') : t('The scan reached its traversal limit; results are partial.')}</p>
      <div className={styles.technologyRow}>{technologies.length ? technologies.map((item) => <span className={styles.techChip} key={`${item.scope}-${item.id}`}>{item.label}<small>{item.scope}</small>{item.evidence.map((evidencePath) => <small key={evidencePath}>{t('Evidence · {path}', { path: evidencePath })}</small>)}</span>) : <span className={styles.mutedText}>{t('Add a supported project marker to see evidence here.')}</span>}</div>
      {!recommendations.scanComplete && <div className={styles.inlineWarning}>{t('Partial scan · review scan diagnostics')}</div>}
    </section>

    <section className={`${styles.panel} ${styles.agentPanel}`}>
      <div className={styles.panelTop}><span className={styles.kicker}>{t('AGENT ADAPTERS')}</span><span className={styles.countMark}>{visibleAgents.length.toString().padStart(2, '0')}</span></div>
      <div className={styles.agentList}>{visibleAgents.map((agent) => <div className={styles.agentRow} key={agent.id}>
        <span className={`${styles.agentGlyph} ${agent.status === 'unsupported' ? styles.agentGlyphMuted : ''}`}>{agent.id.slice(0, 1).toUpperCase()}</span>
        <div className={styles.agentInfo}><strong>{agent.id}</strong><span>{agent.capabilities.length ? agent.capabilities.join(' · ') : statusLabel(agent.status, t)}</span></div>
        <span className={`${styles.pill} ${agent.availability === 'configured' ? styles.pillReady : agent.status === 'unsupported' ? styles.pillMuted : styles.pillQuiet}`}>{statusLabel(agent.availability, t)}</span>
      </div>)}</div>
      <p className={styles.panelFoot}>{t(unavailable === 1 ? '{count} adapter needs attention or is unsupported.' : '{count} adapters need attention or are unsupported.', { count: unavailable })}</p>
    </section>

    <section className={styles.panel}>
      <div className={styles.panelTop}><span className={styles.kicker}>{t('SKILL RECOMMENDATIONS')}</span><span className={styles.countMark}>{recommended.length.toString().padStart(2, '0')}</span></div>
      {recommended.length ? <div className={styles.recommendationList}>{recommended.map((item, index) => <article className={styles.recommendationRow} key={`${item.scope}-${item.identifier}`}>
        <span className={styles.findingIndex}>{String(index + 1).padStart(2, '0')}</span>
        <div><strong>{item.identifier}</strong><p>{item.description}</p><span className={styles.reasonText}>{recommendationReason(item.matchReasonRef, item.matchingEvidence ?? [], item.matchReason, message, t)}</span></div>
        <div className={styles.recommendationAction}><span className={styles.pill}>{statusLabel(item.confidence, t)} · {item.score}</span><button type="button" className={styles.textButton} onClick={() => onActivate({ identifier: item.identifier, name: item.identifier, description: item.description, tags: [], compatibility: [] })}>{t('Review activation →')}</button></div>
      </article>)}</div> : <EmptyInline title={t('No matching Skills')} detail={t('Search the eligible library to browse available Skills.')} />}
    </section>

    <section className={`${styles.panel} ${styles.inventoryPanel}`}>
      <div className={styles.panelTop}><span className={styles.kicker}>{t('PROJECT INVENTORY')}</span><span className={styles.countMark}>{resources.length.toString().padStart(2, '0')}</span></div>
      {resources.length ? <div className={styles.inventoryList}>{resources.map((resource) => <div className={styles.inventoryRow} key={`${resource.target}-${resource.identifier}`}>
        <div className={styles.resourceMain}><span className={styles.pill}>{statusLabel(resource.status, t)}</span><strong>{resource.identifier}</strong><span className={styles.mutedText}>{resource.target}</span><code>{resource.path}</code></div>
        {resource.status === 'managed' && <button className={styles.subtleDangerButton} type="button" disabled={busy} onClick={() => onRemove(resource)}>{t('Plan removal')}</button>}
      </div>)}</div> : <EmptyInline title={t('No project Skills')} detail={t('This project has no entries in supported Skill locations.')} />}
    </section>

    <section className={`${styles.panel} ${styles.diagnosticsStrip}`}>
      <div><span className={styles.kicker}>{t('PROJECT SCAN')}</span><strong>{t(recommendations.diagnostics.length === 1 ? '{count} project scan note' : '{count} project scan notes', { count: recommendations.diagnostics.length })}</strong><span>{t('Doctor findings and repair plans are on Diagnostics.')}</span></div>
      <button className={styles.secondaryButton} type="button" onClick={onDiagnostics}>{t('Open diagnostics')} <span>↗</span></button>
    </section>
  </div>
}

function SkillsPage({ page, recommendations, selectedSkillIDs, selectionAction, unavailableSkillIDs, mutationBlocked, inputsLocked, onRetryRecommendations, onToggleSkill, query, setQuery, onSearch, selected, onSelect, targets, onToggleTarget, supportedTargets, batchTargets, onToggleBatchTarget, batchReplaceConflicts, setBatchReplaceConflicts, batchTargetsValid, batchPreview, onPreviewInstallation, onApplyInstallation, onCancelInstallation, forceReplace, setForceReplace, onActivate, busy }: {
  page: PageState<InstallationCatalog>
  recommendations: PageState<RecommendationResult>
  selectedSkillIDs: string[]
  selectionAction: SkillSelectionAction
  unavailableSkillIDs: string[]
  mutationBlocked: boolean
  inputsLocked: boolean
  onRetryRecommendations: () => void
  onToggleSkill: (identifier: string) => void
  query: string
  setQuery: (value: string) => void
  onSearch: (event: React.FormEvent<HTMLFormElement>) => void
  selected: Skill | null
  onSelect: (skill: Skill | null) => void
  targets: string[]
  onToggleTarget: (target: string) => void
  supportedTargets: InstallationCatalog['targets']
  batchTargets: string[]
  onToggleBatchTarget: (target: string) => void
  batchReplaceConflicts: boolean
  setBatchReplaceConflicts: (value: boolean) => void
  batchTargetsValid: boolean
  batchPreview: InstallationPreview | null
  onPreviewInstallation: () => void
  onApplyInstallation: () => void
  onCancelInstallation: () => void
  forceReplace: boolean
  setForceReplace: (value: boolean) => void
  onActivate: () => void
  busy: boolean
}) {
  const { t, message, installationMessage } = useLanguage()
  return <div className={styles.skillsWorkspace} data-selection-action={selectionAction}>
    <section className={styles.panel} aria-label={t('Recommended Skills')}>
      <div className={styles.panelTop}><span className={styles.kicker}>{t('RECOMMENDED SKILLS')}</span></div>
      {recommendations.loading ? <LoadingState label={t('Reading this project…')} /> : recommendations.error ? <><ErrorState error={recommendations.error} /><button type="button" className={styles.secondaryButton} onClick={onRetryRecommendations}>{t('Retry recommendations')}</button></> : recommendations.data?.scopes.some((scope) => scope.recommendations.length) ? <>
        {!recommendations.data.scanComplete && <div className={styles.inlineWarning}>{t('Partial scan · review scan diagnostics')}</div>}
        {recommendations.data.scopes.map((scope, scopeIndex) => <div key={`${scope.path}-${scopeIndex}`}>
          <p className={styles.mutedText}>{scope.path}</p>
          {scope.recommendations.map((item, itemIndex) => <article className={styles.recommendationRow} key={`${item.identifier}-${itemIndex}`}>
            <input type="checkbox" aria-label={t('Select {identifier}', { identifier: item.identifier })} checked={selectedSkillIDs.includes(item.identifier)} disabled={inputsLocked} onChange={() => onToggleSkill(item.identifier)} />
            <div><strong>{item.identifier}</strong><p>{item.description}</p><span className={styles.reasonText}>{recommendationReason(item.matchReasonRef, item.matchingEvidence ?? [], item.matchReason, message, t)}</span></div>
            <div className={styles.recommendationAction}><span className={styles.pill}>{statusLabel(item.confidence, t)} · {item.score}</span><button type="button" className={styles.textButton} onClick={() => onSelect({ identifier: item.identifier, name: item.identifier, description: item.description, tags: [], compatibility: [] })}>{t('Read details →')}</button></div>
          </article>)}
        </div>)}
      </> : <EmptyInline title={t('No matching Skills')} detail={t('Search the eligible library to browse available Skills.')} />}
    </section>
    <div className={styles.libraryLayout}>
      <section className={`${styles.panel} ${styles.libraryPanel}`} aria-label={t('Eligible library')}>
        <div className={styles.panelTop}><span className={styles.kicker}>{t('ELIGIBLE LIBRARY')}</span><span className={styles.countMark}>{page.data?.skills.length.toString().padStart(2, '0') ?? '—'}</span></div>
        <form className={styles.searchForm} onSubmit={onSearch}><label className={styles.srOnly} htmlFor="skill-search">{t('Search Skills')}</label><input id="skill-search" type="search" placeholder={t('Find by name, tag, or description')} value={query} onChange={(event) => setQuery(event.target.value)} /><button className={styles.secondaryButton} type="submit" disabled={busy || page.loading}>{t('Search')}</button></form>
        {page.loading ? <LoadingState label={t('Searching eligible Skills…')} /> : page.error ? <ErrorState error={page.error} /> : page.data?.skills.length ? <div className={styles.skillList}>{page.data.skills.map((skill) => <div key={skill.identifier}>
          <input type="checkbox" aria-label={t('Select {identifier}', { identifier: skill.identifier })} checked={selectedSkillIDs.includes(skill.identifier)} disabled={inputsLocked} onChange={() => onToggleSkill(skill.identifier)} />
          <button type="button" className={`${styles.skillRow} ${selected?.identifier === skill.identifier ? styles.skillRowActive : ''}`} onClick={() => onSelect(skill)}>
            <span className={styles.skillBullet}>↗</span><span className={styles.skillSummary}><strong>{skill.name || skill.identifier}</strong><span>{skill.description}</span><small>{skill.tags.length ? skill.tags.join(' · ') : t('No tags')}</small><small>{t('Source: {source}', { source: skill.source || t('Local library') })}</small></span><span className={styles.chevron}>›</span>
        </button></div>)}</div> : <EmptyInline title={t('No eligible Skills found')} detail={t('Try another term or add a valid SKILL.md to the configured library.')} />}
      </section>
      <section className={`${styles.panel} ${styles.skillDetailPanel}`} aria-label={t('Skill detail')}>
        {selected ? <>
          <div className={styles.panelTop}><span className={styles.kicker}>{t('SKILL DETAIL / {identifier}', { identifier: selected.identifier })}</span><span className={styles.pill}>{selected.sourceType || selected.provenance || t('Local library')}</span></div>
          <h2>{selected.name || selected.identifier}</h2><p className={styles.detailLead}>{selected.description}</p>
          <div className={styles.metadataRow}><span>{t('Source')} <strong>{selected.source || t('Local library')}</strong></span><span>{t('Tags')} <strong>{selected.tags.join(', ') || t('None')}</strong></span><span>{t('Compatibility')} <strong>{selected.compatibility.join(', ') || t('Not declared')}</strong></span></div>
          <div className={styles.documentLabel}>{t('SKILL.MD CONTENT')} <span>{t('READ ONLY · NEVER EXECUTED')}</span></div>
          <pre className={styles.skillBody}>{selected.body ?? t('Select a Skill to read its content.')}</pre>
        </> : <EmptyState title={t('Select a Skill')} detail={t('Open a catalog entry to inspect its metadata and plan a guarded activation.')} />}
      </section>
    </div>
    <div className={`${styles.panel} ${styles.selectionBar}`}>
      <p data-selection-action={selectionAction} aria-live="polite">{t('{count} selected', { count: selectedSkillIDs.length })}</p>
      {selectedSkillIDs.length > 0 && <section className={styles.selectionControls} aria-label={t('Selected Skills')}>
        <ul>{selectedSkillIDs.map((identifier) => <li key={identifier}>
          <span>{identifier}{unavailableSkillIDs.includes(identifier) ? ` · ${t('Unavailable')}` : ''}</span>
          <button type="button" disabled={inputsLocked} aria-label={t('Deselect {identifier}', { identifier })} onClick={() => onToggleSkill(identifier)}>×</button>
        </li>)}</ul>
        {selectionAction === 'activate' && <>
          <fieldset className={styles.targetFieldset} disabled={inputsLocked}><legend>{t('Activation targets')}</legend><div className={styles.targetChecks}>{['claude-code', 'codex', 'pi'].map((target) => <label key={target}><input type="checkbox" checked={targets.includes(target)} onChange={() => onToggleTarget(target)} /><span>{target}</span></label>)}</div></fieldset>
          <label className={styles.forceCheck}><input type="checkbox" disabled={inputsLocked} checked={forceReplace} onChange={(event) => setForceReplace(event.target.checked)} /><span>{t('Allow replacement of existing conflicts')}</span><small>{t('Requires this separate confirmation and is recorded in the review plan.')}</small></label>
          <button className={styles.primaryButton} type="button" disabled={mutationBlocked || !targets.length} onClick={onActivate}>{t('Review activation plan')}</button>
        </>}
        {selectionAction === 'batch-install' && <>
          <fieldset className={styles.targetFieldset} disabled={inputsLocked}><legend>{t('Installation targets')}</legend><div className={styles.targetChecks}>{supportedTargets.map((target) => <label key={target.id}><input type="checkbox" checked={batchTargets.includes(target.id)} onChange={() => onToggleBatchTarget(target.id)} /><span>{target.label}</span></label>)}</div></fieldset>
          <label className={styles.forceCheck}><input type="checkbox" disabled={inputsLocked} checked={batchReplaceConflicts} onChange={(event) => setBatchReplaceConflicts(event.target.checked)} /><span>{t('Allow replacement of existing conflicts')}</span></label>
          <button className={styles.primaryButton} type="button" disabled={mutationBlocked || !batchTargetsValid} onClick={onPreviewInstallation}>{t('Preview installation')}</button>
        </>}
      </section>}
    </div>
    {batchPreview && <section className={styles.reviewPanel} aria-labelledby="installation-review-title">
      <h2 id="installation-review-title">{t('Review installation')}</h2>
      <p>{batchPreview.skillIds.join(', ')} → {batchPreview.targets.join(', ')}</p>
      <ol className={styles.changeList}>{(batchPreview.plan.changes ?? []).map((change, index) => <li key={`${change.path}-${index}`}><div><strong>{installationMessage(change.action)}</strong>{change.detail && <span>{change.detail}</span>}<code>{change.path}</code></div></li>)}</ol>
      {(batchPreview.plan.warnings ?? []).map((warning, index) => <p className={styles.inlineWarning} key={index}>{installationMessage(warning)}</p>)}
      <div className={styles.reviewActions}><button className={styles.primaryButton} type="button" disabled={mutationBlocked || !batchTargetsValid || !batchPreview.id.trim()} onClick={onApplyInstallation}>{t('Confirm installation')}</button><button className={styles.secondaryButton} type="button" disabled={busy} onClick={onCancelInstallation}>{t('Cancel installation')}</button></div>
    </section>}
  </div>
}

function SubAgentsPage({ page }: { page: PageState<{ definitions: SubAgent[]; diagnostics: SubAgentDiagnostic[] }> }) {
  const { t } = useLanguage()
  if (page.loading && !page.data) return <LoadingState label={t('Reading canonical definitions…')} />
  if (page.error) return <ErrorState error={page.error} />
  if (!page.data) return <EmptyState title={t('SubAgents are unavailable')} detail={t('Try refreshing this view.')} />
  return <div className={styles.documentGrid}>
    <section className={styles.panel}><div className={styles.panelTop}><span className={styles.kicker}>{t('CANONICAL DEFINITIONS')}</span><span className={styles.countMark}>{page.data.definitions.length.toString().padStart(2, '0')}</span></div>
      {page.data.definitions.length ? page.data.definitions.map((definition) => <article className={styles.definitionCard} key={definition.id}><div className={styles.panelTop}><h2>{definition.name}</h2><span className={styles.pill}>v{definition.version}</span></div><p className={styles.roleLine}>{definition.id} · {definition.role}</p><p className={styles.instructions}>{definition.instructions}</p><div className={styles.metadataRow}><span>{t('Skills ')}<strong>{definition.skills?.join(', ') || t('None')}</strong></span><span>{t('Agents ')}<strong>{definition.compatibility?.agents?.join(', ') || t('Any')}</strong></span><span>{t('Capabilities ')}<strong>{definition.requiredCapabilities?.join(', ') || t('None')}</strong></span></div></article>) : <EmptyInline title={t('No canonical SubAgents')} detail={t('Definitions are read from the configured Agent Manager data root.')} />}
    </section>
    <section className={styles.panel}><div className={styles.panelTop}><span className={styles.kicker}>{t('VALIDATION NOTES')}</span><span className={styles.countMark}>{page.data.diagnostics.length.toString().padStart(2, '0')}</span></div>
      {page.data.diagnostics.length ? page.data.diagnostics.map((finding) => <div className={styles.findingRow} key={`${finding.path}-${finding.message}`}><strong>{finding.id || t('Invalid definition')}</strong><p>{finding.message}</p><code>{finding.path}</code></div>) : <EmptyInline title={t('All definitions are valid')} detail={t('SubAgent inspection is read only; this view has no installation controls.')} />}
    </section>
  </div>
}

function DiagnosticsPage({ page, scanDiagnostics, onRefresh, onRepair, busy }: {
  page: PageState<Finding[]>
  scanDiagnostics: Finding[]
  onRefresh: () => void
  onRepair: (finding: Finding) => void
  busy: boolean
}) {
  const { t, message } = useLanguage()
  return <div className={styles.documentGrid}>
    <section className={styles.panel}>
      <div className={styles.panelTop}><h2 className={styles.panelTitle}>{t('Doctor findings')}</h2><div className={styles.panelTopActions}><span className={styles.countMark}>{(page.data?.length ?? 0).toString().padStart(2, '0')}</span><button className={styles.secondaryButton} type="button" onClick={onRefresh} disabled={busy}>↻ <span>{t('Run scan')}</span></button></div></div>
      <p className={styles.detailLead}>{t('Available repairs are previewed, require confirmation, and are recorded for Undo.')}</p>
      {page.loading ? <LoadingState label={t('Scanning configured resources…')} /> : page.error ? <ErrorState error={page.error} /> : page.data?.length ? page.data.map((finding, index) => <div className={styles.findingRow} key={`${finding.path}-${index}`}>
        <span className={styles.findingIndex}>{String(index + 1).padStart(2, '0')}</span>
        <div>
          <div className={styles.findingHeading}><strong>{message(finding.messageRef, finding.message)}</strong><span className={`${styles.pill} ${finding.repairAction ? styles.pillReady : styles.pillQuiet}`}>{t(finding.repairAction ? 'Repair available' : 'Manual review')}</span></div>
          <code>{finding.path}</code>
          {(finding.guidanceRef || finding.guidance) && <p>{message(finding.guidanceRef, finding.guidance ?? '')}</p>}
          {finding.repairAction === 'reconcile' && <button className={styles.textButton} type="button" disabled={busy} onClick={() => onRepair(finding)}>{t('Preview reconciliation')}</button>}
          {finding.repairAction === 'add_gitignore' && <button className={styles.textButton} type="button" disabled={busy} onClick={() => onRepair(finding)}>{t('Add ignore rule')}</button>}
        </div>
      </div>) : <EmptyInline title={t('No Doctor findings')} detail={t('The Doctor scan completed without findings.')} />}
    </section>

    <section className={styles.panel}>
      <div className={styles.panelTop}><h2 className={styles.panelTitle}>{t('Project scan notes')}</h2><span className={styles.countMark}>{scanDiagnostics.length.toString().padStart(2, '0')}</span></div>
      {scanDiagnostics.length ? scanDiagnostics.map((finding, index) => <div className={styles.findingRow} key={`${finding.path}-${index}`}>
        <span className={styles.findingIndex}>{String(index + 1).padStart(2, '0')}</span>
        <div><strong>{message(finding.messageRef, finding.message)}</strong><code>{finding.path}</code></div>
      </div>) : <EmptyInline title={t('No project scan notes')} detail={t('The static project scan completed without notes.')} />}
    </section>
  </div>
}

function OperationsPage({ page, onUndo, busy }: { page: PageState<LatestOperation>; onUndo: () => void; busy: boolean }) {
  const { locale, t, message } = useLanguage()
  if (page.loading && page.data === null) return <LoadingState label={t('Reading operation journal…')} />
  if (page.error) return <ErrorState error={page.error} />
  return <section className={styles.panel}><div className={styles.panelTop}><span className={styles.kicker}>{t('LATEST REVERSIBLE OPERATION')}</span><span className={styles.pill}>{t('PROJECT JOURNAL')}</span></div>
    {page.data ? <div className={styles.operationCard}><span className={styles.operationGlyph}>↶</span><div><span className={styles.kicker}>{resourceKindLabel(page.data.resourceKind, t)} · {new Date(page.data.at).toLocaleString(locale)}</span><h2>{message(page.data.operationRef, page.data.operation)}</h2><p>{t('{count} paths in the available Undo preview.', { count: page.data.undoPreview?.changes.length ?? 0 })}</p></div>{page.data.undoAvailable && <button className={styles.secondaryButton} type="button" disabled={busy} onClick={onUndo}>{t('Review Undo')}</button>}</div> : <EmptyInline title={t('No operation journal yet')} detail={t('Successful guarded operations appear here with an Undo preview.')} />}
  </section>
}

function PlanReview({ plan, busy, canExecute = true, onExecute, onCancel }: { plan: OperationPlan; busy: boolean; canExecute?: boolean; onExecute: () => void; onCancel: () => void }) {
  const { locale, t, message } = useLanguage()
  return <section className={styles.reviewPanel} aria-labelledby="review-title">
    <div className={styles.reviewTop}><div><span className={styles.kicker}>{t('REVIEW BEFORE APPLYING')}</span><h2 id="review-title">{message(plan.operationRef, plan.operation)}</h2></div><span className={styles.reviewExpiry}>{t('Expires {time}', { time: new Date(plan.expiresAt).toLocaleTimeString(locale) })}</span></div>
    {plan.forceReplacementConfirmed && <div className={styles.forceNotice}>{t('Force replacement confirmed for this plan. Existing conflicting content may be replaced.')}</div>}
    <ol className={styles.changeList}>{plan.changes.map((change, index) => <li key={`${change.path}-${index}`}><span className={styles.changeNumber}>{String(index + 1).padStart(2, '0')}</span><div><strong>{message(change.actionRef, change.action)}</strong>{change.detail && <span>{change.detail}</span>}<code>{change.path}</code></div></li>)}</ol>
    {plan.warnings.map((warning, index) => <p className={styles.inlineWarning} key={`${warning}-${index}`}>{message(plan.warningRefs?.[index] ?? undefined, warning)}</p>)}
    <div className={styles.reviewActions}><button className={styles.primaryButton} type="button" disabled={busy || !canExecute} onClick={onExecute}>{busy ? t('Applying…') : t('Confirm and apply')}</button><button className={styles.secondaryButton} type="button" disabled={busy} onClick={onCancel}>{t('Cancel plan')}</button></div>
  </section>
}

function LoadingState({ label }: { label: string }) { return <div className={styles.stateBox} role="status"><span className={styles.loaderMark} />{label}</div> }
function ErrorState({ error }: { error: unknown }) { const { t, error: formatError } = useLanguage(); return <div className={`${styles.stateBox} ${styles.errorState}`} role="alert"><strong>{t('Could not load this view')}</strong><span>{formatError(error)}</span></div> }
function EmptyState({ title, detail }: { title: string; detail: string }) { return <div className={styles.stateBox}><strong>{title}</strong><span>{detail}</span></div> }
function EmptyInline({ title, detail }: { title: string; detail: string }) { return <div className={styles.emptyInline}><strong>{title}</strong><span>{detail}</span></div> }

function statusLabel(value: string, t: (source: string) => string) {
  const labels: Record<string, string> = {
    configured: 'Configured',
    unavailable: 'Unavailable',
    unsupported: 'Unsupported',
    detected: 'Detected',
    managed: 'Managed',
    unmanaged: 'Unmanaged',
    orphaned: 'Orphaned',
    tracked: 'Tracked',
    ignored: 'Ignored',
    'would be tracked': 'Would be tracked',
    high: 'High',
    medium: 'Medium',
    low: 'Low',
  }
  return labels[value] ? t(labels[value]) : value
}

function resourceKindLabel(value: string, t: (source: string) => string) {
  const labels: Record<string, string> = { skill: 'Skill resource', subagent: 'SubAgent resource' }
  return labels[value] ? t(labels[value]) : value
}

function operationLabel(value: string, translateMessage: (ref: MessageRef | undefined, fallback: string) => string) {
  const keys: Record<string, string> = {
    activate: 'plan.operation.activate',
    'activate selected skills': 'plan.operation.activate_selected_skills',
    remove: 'plan.operation.remove',
    reconcile: 'plan.operation.reconcile',
    'update managed-link Git guidance': 'plan.operation.gitignore',
  }
  if (value.startsWith('undo ')) {
    const operation = keys[value.slice('undo '.length)]
    return operation
      ? translateMessage({ key: 'plan.operation.undo_action', args: { operation } }, value)
      : translateMessage({ key: 'plan.operation.undo' }, value)
  }
  if (value === 'undo') return translateMessage({ key: 'plan.operation.undo' }, value)
  const key = keys[value]
  return key ? translateMessage({ key }, value) : value
}

function recommendationReason(
  ref: MessageRef | undefined,
  evidence: { technology: string; field: string }[],
  fallback: string,
  translateMessage: (ref: MessageRef | undefined, fallback: string) => string,
  t: (source: string, values?: Record<string, string | number>) => string,
) {
  if (ref?.key === 'recommendation.matched_evidence') {
    if (!evidence.length) return fallback
    const detail = evidence.map((item) => `${item.technology} · ${item.field}`).join(', ')
    return translateMessage({ ...ref, args: { ...ref.args, evidence: detail } }, t('Matched project evidence: {evidence}', { evidence: detail }))
  }
  return translateMessage(ref, fallback)
}

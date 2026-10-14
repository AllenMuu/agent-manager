import { useCallback, useEffect, useMemo, useState } from 'react'
import { get, post } from './api'
import { bootstrapSession } from './session'
import { useLanguage } from './i18n'
import type {
  Agent,
  Finding,
  LatestOperation,
  MessageRef,
  OperationPlan,
  Project,
  RecommendationResult,
  Resource,
  Skill,
  SubAgent,
  SubAgentDiagnostic,
} from './types'
import styles from './App.module.css'

type View = 'workspace' | 'skills' | 'subagents' | 'diagnostics' | 'operations' | 'install'
type PageState<T> = { data: T | null; loading: boolean; error: unknown | null }
type PlanEnvelope = { plan: OperationPlan }
type Toast = { kind: 'success'; text: string; values?: Record<string, string | number> } | { kind: 'error'; error: unknown }

const navItems: { id: View; label: string; eyebrow: string }[] = [
  { id: 'workspace', label: 'Workspace', eyebrow: '01' },
  { id: 'skills', label: 'Skills', eyebrow: '02' },
  { id: 'subagents', label: 'SubAgents', eyebrow: '03' },
  { id: 'diagnostics', label: 'Diagnostics', eyebrow: '04' },
  { id: 'operations', label: 'Operations', eyebrow: '05' },
  { id: 'install', label: 'Skill installation', eyebrow: '06' },
]

const initialPage = <T,>(): PageState<T> => ({ data: null, loading: false, error: null })

export function App() {
  const { t, message: translateMessage, error: formatError } = useLanguage()
  const [token] = useState(() => bootstrapSession())
  const [project, setProject] = useState<Project | null>(null)
  const [projectPath, setProjectPath] = useState('')
  const [view, setView] = useState<View>('workspace')
  const [workspace, setWorkspace] = useState<PageState<{ agents: Agent[]; recommendations: RecommendationResult; resources: Resource[] }>>(initialPage)
  const [skills, setSkills] = useState<PageState<Skill[]>>(initialPage)
  const [subagents, setSubagents] = useState<PageState<{ definitions: SubAgent[]; diagnostics: SubAgentDiagnostic[] }>>(initialPage)
  const [findings, setFindings] = useState<PageState<Finding[]>>(initialPage)
  const [latest, setLatest] = useState<PageState<LatestOperation>>(initialPage)
  const [query, setQuery] = useState('')
  const [selectedSkill, setSelectedSkill] = useState<Skill | null>(null)
  const [targets, setTargets] = useState<string[]>(['codex'])
  const [forceReplace, setForceReplace] = useState(false)
  const [pendingPlan, setPendingPlan] = useState<OperationPlan | null>(null)
  const [toast, setToast] = useState<Toast | null>(null)
  const [busy, setBusy] = useState(false)

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
    setBusy(true)
    setToast(null)
    try {
      onSuccess(await action())
    } catch (error) {
      if (onError) onError(error)
      else setToast({ kind: 'error', error })
    } finally {
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
    if (!token) return
    setSkills((current) => ({ ...current, loading: true, error: null }))
    setSelectedSkill(null)
    try {
      const result = await get<{ skills: Skill[] }>(token, `/api/v1/skills?q=${encodeURIComponent(searchTerm)}`)
      setSkills({ data: result.skills, loading: false, error: null })
    } catch (error) {
      setSkills({ data: null, loading: false, error })
    }
  }

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

  const createActivationPlan = async (skill: Skill) => {
    if (!token || !project) return
    await runRequest(
      () => post<PlanEnvelope>(token, `${projectBase}/plans/activate`, {
        skillIdentifiers: [skill.identifier],
        targetAgents: targets,
        ...(forceReplace ? { conflictStrategy: 'replace', forceConfirmed: true } : {}),
      }),
      (result) => setPendingPlan(result.plan),
    )
  }

  const createRemovalPlan = async (resource: Resource) => {
    if (!token || !project) return
    await runRequest(
      () => post<PlanEnvelope>(token, `${projectBase}/plans/remove`, { skillIdentifier: resource.identifier, targetAgent: resource.target }),
      (result) => setPendingPlan(result.plan),
    )
  }

  const createUndoPlan = async () => {
    if (!token || !project) return
    await runRequest(
      () => post<PlanEnvelope>(token, `${projectBase}/plans/undo`, {}),
      (result) => setPendingPlan(result.plan),
    )
  }

  const executePlan = async () => {
    if (!token || !pendingPlan) return
    await runRequest(
      () => post<{ operation: string; status: string }>(token, `/api/v1/plans/${pendingPlan.id}/execute`, {}),
      (result) => {
        setPendingPlan(null)
        setToast({ kind: 'success', text: '{operation} completed and journaled.', values: { operation: operationLabel(result.operation, translateMessage) } })
        if (project) void loadWorkspace(project.id)
        void loadLatest()
      },
      (error) => {
        setPendingPlan(null)
        setToast({ kind: 'error', error })
        if (project) void loadWorkspace(project.id)
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
        setToast({ kind: 'success', text: 'The pending plan was cancelled.' })
      },
    )
  }

  const navigate = (next: View) => {
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

      {!project ? <ProjectRegistration path={projectPath} setPath={setProjectPath} onSubmit={registerProject} busy={busy} /> : <>
        {pendingPlan && <PlanReview plan={pendingPlan} busy={busy} onExecute={() => void executePlan()} onCancel={() => void cancelPlan()} />}
        {view === 'workspace' && <Workspace
          page={workspace}
          onActivate={openWorkspaceSkill}
          onRemove={(resource) => void createRemovalPlan(resource)}
          onDiagnostics={() => navigate('diagnostics')}
        />}
        {view === 'skills' && <SkillsPage
          page={skills}
          query={query}
          setQuery={setQuery}
          onSearch={(event) => { event.preventDefault(); void loadSkills(query) }}
          selected={selectedSkill}
          onSelect={(skill) => { if (skill) void openSkill(skill); else setSelectedSkill(null) }}
          targets={targets}
          onToggleTarget={(target) => setTargets((current) => current.includes(target) ? current.filter((item) => item !== target) : [...current, target])}
          forceReplace={forceReplace}
          setForceReplace={setForceReplace}
          onActivate={(skill) => void createActivationPlan(skill)}
          busy={busy}
        />}
        {view === 'subagents' && <SubAgentsPage page={subagents} />}
        {view === 'diagnostics' && <DiagnosticsPage page={findings} onRefresh={() => void loadDiagnostics()} />}
        {view === 'operations' && <OperationsPage page={latest} onUndo={() => void createUndoPlan()} busy={busy} />}
        {view === 'install' && <iframe className={styles.installationFrame} title={t('Skill installation')} src={`/install/?embedded=1#session=${encodeURIComponent(token)}`} />}
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

function ProjectRegistration({ path, setPath, onSubmit, busy }: { path: string; setPath: (path: string) => void; onSubmit: (event: React.FormEvent<HTMLFormElement>) => void; busy: boolean }) {
  const { t } = useLanguage()
  return <section className={styles.registerPanel}>
    <div className={styles.registerIndex}>01 <span>/ {t('PROJECT')}</span></div>
    <div className={styles.registerCopy}><span className={styles.kicker}>{t('START WITH A PROJECT')}</span><h2>{t('Choose one local workspace.')}</h2><p>{t('The console inspects this directory through Agent Manager’s existing read-only services. It does not expose general file browsing.')}</p></div>
    <form className={styles.registerForm} onSubmit={onSubmit}>
      <label htmlFor="project-path">{t('Project directory')}</label>
      <div className={styles.inputAction}><input id="project-path" value={path} onChange={(event) => setPath(event.target.value)} placeholder="/Users/you/work/my-project" autoComplete="off" required /><button className={styles.primaryButton} type="submit" disabled={busy}>{busy ? t('Checking…') : t('Inspect project')}</button></div>
      <p>{t('Paste an absolute path or a path relative to where the console was started.')}</p>
    </form>
    <div className={styles.registerStamp}><span>01</span><span>{t('SESSION')}<br />{t('BOUND')}</span></div>
  </section>
}

function Workspace({ page, onActivate, onRemove, onDiagnostics }: {
  page: PageState<{ agents: Agent[]; recommendations: RecommendationResult; resources: Resource[] }>
  onActivate: (skill: Skill) => void
  onRemove: (resource: Resource) => void
  onDiagnostics: () => void
}) {
  const { t, message } = useLanguage()
  if (page.loading && !page.data) return <LoadingState label={t('Reading this project…')} />
  if (page.error) return <ErrorState error={page.error} />
  if (!page.data) return <EmptyState title={t('Workspace is ready')} detail={t('Refresh to read the project inventory.')} />
  const { agents, recommendations, resources } = page.data
  const recommended = recommendations.scopes.flatMap((scope) => scope.recommendations.map((item) => ({ ...item, scope: scope.path })))
  const technologies = recommendations.scopes.flatMap((scope) => scope.technologies.map((technology) => ({
    ...technology,
    scope: scope.path,
    evidence: scope.evidence.filter((item) => item.technology === technology.id).map((item) => item.path),
  })))
  const unavailable = agents.filter((agent) => agent.availability === 'unavailable' || agent.status === 'unsupported').length
  return <div className={styles.workspaceGrid}>
    <section className={`${styles.panel} ${styles.summaryPanel}`}>
      <div className={styles.panelTop}><span className={styles.kicker}>{t('PROJECT SNAPSHOT')}</span><span className={styles.statusDot}><i /> {t('Read only')}</span></div>
      <h2>{technologies.length ? t('{count} signals found', { count: technologies.length }) : t('No stack signals yet')}</h2>
      <p>{recommendations.scanComplete ? t('Static markers were read from the project tree.') : t('The scan reached its traversal limit; results are partial.')}</p>
      <div className={styles.technologyRow}>{technologies.length ? technologies.map((item) => <span className={styles.techChip} key={`${item.scope}-${item.id}`}>{item.label}<small>{item.scope}</small>{item.evidence.map((evidencePath) => <small key={evidencePath}>{t('Evidence · {path}', { path: evidencePath })}</small>)}</span>) : <span className={styles.mutedText}>{t('Add a supported project marker to see evidence here.')}</span>}</div>
      {!recommendations.scanComplete && <div className={styles.inlineWarning}>{t('Partial scan · review scan diagnostics')}</div>}
    </section>

    <section className={`${styles.panel} ${styles.agentPanel}`}>
      <div className={styles.panelTop}><span className={styles.kicker}>{t('AGENT ADAPTERS')}</span><span className={styles.countMark}>{agents.length.toString().padStart(2, '0')}</span></div>
      <div className={styles.agentList}>{agents.map((agent) => <div className={styles.agentRow} key={agent.id}>
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
        {resource.status === 'managed' && <button className={styles.subtleDangerButton} type="button" onClick={() => onRemove(resource)}>{t('Plan removal')}</button>}
      </div>)}</div> : <EmptyInline title={t('No project Skills')} detail={t('This project has no entries in supported Skill locations.')} />}
    </section>

    <section className={`${styles.panel} ${styles.diagnosticsStrip}`}>
      <div><span className={styles.kicker}>{t('DIAGNOSTICS')}</span><strong>{t('{count} scan notes', { count: recommendations.diagnostics.length })}</strong><span>{t('Doctor findings are available in a read-only view.')}</span></div>
      <button className={styles.secondaryButton} type="button" onClick={onDiagnostics}>{t('Open diagnostics')} <span>↗</span></button>
    </section>
  </div>
}

function SkillsPage({ page, query, setQuery, onSearch, selected, onSelect, targets, onToggleTarget, forceReplace, setForceReplace, onActivate, busy }: {
  page: PageState<Skill[]>
  query: string
  setQuery: (value: string) => void
  onSearch: (event: React.FormEvent<HTMLFormElement>) => void
  selected: Skill | null
  onSelect: (skill: Skill | null) => void
  targets: string[]
  onToggleTarget: (target: string) => void
  forceReplace: boolean
  setForceReplace: (value: boolean) => void
  onActivate: (skill: Skill) => void
  busy: boolean
}) {
  const { t } = useLanguage()
  return <div className={styles.libraryLayout}>
    <section className={`${styles.panel} ${styles.libraryPanel}`}>
      <div className={styles.panelTop}><span className={styles.kicker}>{t('ELIGIBLE LIBRARY')}</span><span className={styles.countMark}>{page.data?.length.toString().padStart(2, '0') ?? '—'}</span></div>
      <form className={styles.searchForm} onSubmit={onSearch}><label className={styles.srOnly} htmlFor="skill-search">{t('Search Skills')}</label><input id="skill-search" type="search" placeholder={t('Find by name, tag, or description')} value={query} onChange={(event) => setQuery(event.target.value)} /><button className={styles.secondaryButton} type="submit">{t('Search')}</button></form>
      {page.loading ? <LoadingState label={t('Searching eligible Skills…')} /> : page.error ? <ErrorState error={page.error} /> : page.data?.length ? <div className={styles.skillList}>{page.data.map((skill) => <button type="button" className={`${styles.skillRow} ${selected?.identifier === skill.identifier ? styles.skillRowActive : ''}`} key={skill.identifier} onClick={() => onSelect(skill)}>
        <span className={styles.skillBullet}>↗</span><span className={styles.skillSummary}><strong>{skill.name || skill.identifier}</strong><span>{skill.description}</span><small>{skill.tags.length ? skill.tags.join(' · ') : t('No tags')}</small></span><span className={styles.chevron}>›</span>
      </button>)}</div> : <EmptyInline title={t('No eligible Skills found')} detail={t('Try another term or add a valid SKILL.md to the configured library.')} />}
    </section>
    <section className={`${styles.panel} ${styles.skillDetailPanel}`}>
      {selected ? <>
        <div className={styles.panelTop}><span className={styles.kicker}>{t('SKILL DETAIL / {identifier}', { identifier: selected.identifier })}</span><span className={styles.pill}>{selected.provenance || t('Local library')}</span></div>
        <h2>{selected.name || selected.identifier}</h2><p className={styles.detailLead}>{selected.description}</p>
        <div className={styles.metadataRow}><span>{t('Tags')} <strong>{selected.tags.join(', ') || t('None')}</strong></span><span>{t('Compatibility')} <strong>{selected.compatibility.join(', ') || t('Not declared')}</strong></span></div>
        <div className={styles.documentLabel}>{t('SKILL.MD CONTENT')} <span>{t('READ ONLY · NEVER EXECUTED')}</span></div>
        <pre className={styles.skillBody}>{selected.body ?? t('Select a Skill to read its content.')}</pre>
        <fieldset className={styles.targetFieldset}><legend>{t('Activation targets')}</legend><div className={styles.targetChecks}>{['claude-code', 'codex', 'pi'].map((target) => <label key={target}><input type="checkbox" checked={targets.includes(target)} onChange={() => onToggleTarget(target)} /><span>{target}</span></label>)}</div></fieldset>
        <label className={styles.forceCheck}><input type="checkbox" checked={forceReplace} onChange={(event) => setForceReplace(event.target.checked)} /><span>{t('Allow replacement of existing conflicts')}</span><small>{t('Requires this separate confirmation and is recorded in the review plan.')}</small></label>
        <button className={styles.primaryButton} type="button" disabled={busy || !targets.length} onClick={() => onActivate(selected)}>{busy ? t('Preparing preview…') : t('Review activation plan')}</button>
      </> : <EmptyState title={t('Select a Skill')} detail={t('Open a catalog entry to inspect its metadata and plan a guarded activation.')} />}
    </section>
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

function DiagnosticsPage({ page, onRefresh }: { page: PageState<Finding[]>; onRefresh: () => void }) {
  const { t, message } = useLanguage()
  return <section className={styles.panel}><div className={styles.panelTop}><span className={styles.kicker}>{t('READ-ONLY DOCTOR')}</span><button className={styles.secondaryButton} type="button" onClick={onRefresh}>↻ <span>{t('Run scan')}</span></button></div>
    <p className={styles.detailLead}>{t('Findings come from the existing doctor checks. This scan never reconciles project state.')}</p>
    {page.loading ? <LoadingState label={t('Scanning configured resources…')} /> : page.error ? <ErrorState error={page.error} /> : page.data?.length ? page.data.map((finding, index) => <div className={styles.findingRow} key={`${finding.path}-${index}`}><span className={styles.findingIndex}>{String(index + 1).padStart(2, '0')}</span><div><strong>{message(finding.messageRef, finding.message)}</strong><code>{finding.path}</code></div></div>) : <EmptyInline title={t('No findings')} detail={t('The current read-only doctor scan returned no issues.')} />}
  </section>
}

function OperationsPage({ page, onUndo, busy }: { page: PageState<LatestOperation>; onUndo: () => void; busy: boolean }) {
  const { locale, t, message } = useLanguage()
  if (page.loading && page.data === null) return <LoadingState label={t('Reading operation journal…')} />
  if (page.error) return <ErrorState error={page.error} />
  return <section className={styles.panel}><div className={styles.panelTop}><span className={styles.kicker}>{t('LATEST REVERSIBLE OPERATION')}</span><span className={styles.pill}>{t('PROJECT JOURNAL')}</span></div>
    {page.data ? <div className={styles.operationCard}><span className={styles.operationGlyph}>↶</span><div><span className={styles.kicker}>{resourceKindLabel(page.data.resourceKind, t)} · {new Date(page.data.at).toLocaleString(locale)}</span><h2>{message(page.data.operationRef, page.data.operation)}</h2><p>{t('{count} paths in the available Undo preview.', { count: page.data.undoPreview?.changes.length ?? 0 })}</p></div>{page.data.undoAvailable && <button className={styles.secondaryButton} type="button" disabled={busy} onClick={onUndo}>{t('Review Undo')}</button>}</div> : <EmptyInline title={t('No operation journal yet')} detail={t('Successful guarded operations appear here with an Undo preview.')} />}
  </section>
}

function PlanReview({ plan, busy, onExecute, onCancel }: { plan: OperationPlan; busy: boolean; onExecute: () => void; onCancel: () => void }) {
  const { locale, t, message } = useLanguage()
  return <section className={styles.reviewPanel} aria-labelledby="review-title">
    <div className={styles.reviewTop}><div><span className={styles.kicker}>{t('REVIEW BEFORE APPLYING')}</span><h2 id="review-title">{message(plan.operationRef, plan.operation)}</h2></div><span className={styles.reviewExpiry}>{t('Expires {time}', { time: new Date(plan.expiresAt).toLocaleTimeString(locale) })}</span></div>
    {plan.forceReplacementConfirmed && <div className={styles.forceNotice}>{t('Force replacement confirmed for this plan. Existing conflicting content may be replaced.')}</div>}
    <ol className={styles.changeList}>{plan.changes.map((change, index) => <li key={`${change.path}-${index}`}><span className={styles.changeNumber}>{String(index + 1).padStart(2, '0')}</span><div><strong>{message(change.actionRef, change.action)}</strong>{change.detail && <span>{change.detail}</span>}<code>{change.path}</code></div></li>)}</ol>
    {plan.warnings.map((warning, index) => <p className={styles.inlineWarning} key={`${warning}-${index}`}>{message(plan.warningRefs?.[index] ?? undefined, warning)}</p>)}
    <div className={styles.reviewActions}><button className={styles.primaryButton} type="button" disabled={busy} onClick={onExecute}>{busy ? t('Applying…') : t('Confirm and apply')}</button><button className={styles.secondaryButton} type="button" disabled={busy} onClick={onCancel}>{t('Cancel plan')}</button></div>
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

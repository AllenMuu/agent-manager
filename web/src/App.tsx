import { useCallback, useEffect, useMemo, useState } from 'react'
import { APIError, get, post } from './api'
import { bootstrapSession } from './session'
import type {
  Agent,
  Finding,
  LatestOperation,
  OperationPlan,
  Project,
  RecommendationResult,
  Resource,
  Skill,
  SubAgent,
  SubAgentDiagnostic,
} from './types'
import styles from './App.module.css'

type View = 'workspace' | 'skills' | 'subagents' | 'diagnostics' | 'operations'
type PageState<T> = { data: T | null; loading: boolean; error: string }
type PlanEnvelope = { plan: OperationPlan }

const navItems: { id: View; label: string; eyebrow: string }[] = [
  { id: 'workspace', label: 'Workspace', eyebrow: '01' },
  { id: 'skills', label: 'Skills', eyebrow: '02' },
  { id: 'subagents', label: 'SubAgents', eyebrow: '03' },
  { id: 'diagnostics', label: 'Diagnostics', eyebrow: '04' },
  { id: 'operations', label: 'Operations', eyebrow: '05' },
]

const initialPage = <T,>(): PageState<T> => ({ data: null, loading: false, error: '' })

export function App() {
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
  const [message, setMessage] = useState<{ kind: 'success' | 'error'; text: string } | null>(null)
  const [busy, setBusy] = useState(false)

  const projectBase = project ? `/api/v1/projects/${project.id}` : ''

  const loadSystem = useCallback(async () => {
    if (!token) return
    try {
      const result = await get<{ project?: Project }>(token, '/api/v1/system')
      if (result.project) setProject(result.project)
    } catch (error) {
      setMessage({ kind: 'error', text: errorMessage(error) })
    }
  }, [token])

  const loadWorkspace = useCallback(async (id: string) => {
    if (!token) return
    setWorkspace((current) => ({ ...current, loading: true, error: '' }))
    try {
      const [agentResult, recommendationResult, resourceResult] = await Promise.all([
        get<{ agents: Agent[] }>(token, `/api/v1/projects/${id}/agents`),
        get<RecommendationResult>(token, `/api/v1/projects/${id}/recommendations`),
        get<{ resources: Resource[] }>(token, `/api/v1/projects/${id}/resources`),
      ])
      setWorkspace({ data: { agents: agentResult.agents, recommendations: recommendationResult, resources: resourceResult.resources }, loading: false, error: '' })
    } catch (error) {
      setWorkspace({ data: null, loading: false, error: errorMessage(error) })
    }
  }, [token])

  useEffect(() => { void loadSystem() }, [loadSystem])
  useEffect(() => {
    if (project && view === 'workspace') void loadWorkspace(project.id)
  }, [project, view, loadWorkspace])

  const runRequest = useCallback(async <T,>(action: () => Promise<T>, onSuccess: (value: T) => void, onError?: (error: unknown) => void) => {
    setBusy(true)
    setMessage(null)
    try {
      onSuccess(await action())
    } catch (error) {
      if (onError) onError(error)
      else setMessage({ kind: 'error', text: errorMessage(error) })
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
        setMessage({ kind: 'success', text: `Registered ${result.project.name}.` })
      },
    )
  }

  const loadSkills = async (searchTerm = query) => {
    if (!token) return
    setSkills((current) => ({ ...current, loading: true, error: '' }))
    setSelectedSkill(null)
    try {
      const result = await get<{ skills: Skill[] }>(token, `/api/v1/skills?q=${encodeURIComponent(searchTerm)}`)
      setSkills({ data: result.skills, loading: false, error: '' })
    } catch (error) {
      setSkills({ data: null, loading: false, error: errorMessage(error) })
    }
  }

  const openSkill = async (skill: Skill) => {
    setSelectedSkill(skill)
    if (!token) return
    try {
      const result = await get<{ skill: Skill }>(token, `/api/v1/skills/${encodeURIComponent(skill.identifier)}`)
      setSelectedSkill(result.skill)
    } catch (error) {
      setMessage({ kind: 'error', text: errorMessage(error) })
    }
  }

  const openWorkspaceSkill = (skill: Skill) => {
    setView('skills')
    void openSkill(skill)
  }

  const loadSubAgents = async () => {
    if (!token) return
    setSubagents((current) => ({ ...current, loading: true, error: '' }))
    try {
      const result = await get<{ definitions: SubAgent[]; diagnostics: SubAgentDiagnostic[] }>(token, '/api/v1/subagents')
      setSubagents({ data: result, loading: false, error: '' })
    } catch (error) {
      setSubagents({ data: null, loading: false, error: errorMessage(error) })
    }
  }

  const loadDiagnostics = async () => {
    if (!token || !projectBase) return
    setFindings((current) => ({ ...current, loading: true, error: '' }))
    try {
      const result = await get<{ findings: Finding[] }>(token, `${projectBase}/diagnostics`)
      setFindings({ data: result.findings, loading: false, error: '' })
    } catch (error) {
      setFindings({ data: null, loading: false, error: errorMessage(error) })
    }
  }

  const loadLatest = async () => {
    if (!token || !projectBase) return
    setLatest((current) => ({ ...current, loading: true, error: '' }))
    try {
      const result = await get<{ latest: LatestOperation }>(token, `${projectBase}/operations/latest`)
      setLatest({ data: result.latest, loading: false, error: '' })
    } catch (error) {
      setLatest({ data: null, loading: false, error: errorMessage(error) })
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
        setMessage({ kind: 'success', text: `${result.operation} completed and journaled.` })
        if (project) void loadWorkspace(project.id)
        void loadLatest()
      },
      (error) => {
        setPendingPlan(null)
        setMessage({ kind: 'error', text: errorMessage(error) })
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
        setMessage({ kind: 'success', text: 'The pending plan was cancelled.' })
      },
    )
  }

  const navigate = (next: View) => {
    setView(next)
    setMessage(null)
    setSelectedSkill(null)
  }

  const pageTitle = useMemo(() => navItems.find((item) => item.id === view)?.label ?? 'Workspace', [view])

  if (!token) {
    return <main className={styles.sessionPage}><div className={styles.sessionCard}>
      <span className={styles.kicker}>LOCAL SESSION</span>
      <h1>Open the console entry link</h1>
      <p>The session key is kept in this browser tab. Start <code>agent-manager web</code> and open the URL it prints.</p>
    </div></main>
  }

  return <div className={styles.appShell}>
    <header className={styles.mobileHeader}>
      <div className={styles.brandMark} aria-hidden="true">A<span>.</span></div>
      <div><p className={styles.brandTitle}>Agent Manager</p><p className={styles.brandMeta}>LOCAL CONSOLE</p></div>
      <span className={styles.localBadge}><i /> Local</span>
    </header>
    <aside className={styles.sidebar} aria-label="Main navigation">
      <div className={styles.brandBlock}>
        <div className={styles.brandMark} aria-hidden="true">A<span>.</span></div>
        <div><p className={styles.brandTitle}>Agent Manager</p><p className={styles.brandMeta}>LOCAL CONSOLE</p></div>
      </div>
      <div className={styles.navLabel}>PROJECT DESK</div>
      <nav className={styles.navList}>
        {navItems.map((item) => <button key={item.id} type="button" className={`${styles.navItem} ${view === item.id ? styles.navItemActive : ''}`} onClick={() => navigate(item.id)} aria-current={view === item.id ? 'page' : undefined}>
          <span className={styles.navIndex}>{item.eyebrow}</span><span>{item.label}</span>
        </button>)}
      </nav>
      <div className={styles.sidebarFoot}>
        <span className={styles.localBadge}><i /> Local only</span>
        <p>Project changes are reviewed before they are applied.</p>
      </div>
    </aside>

    <main className={styles.mainArea}>
      <div className={styles.topline}>
        <span>{project?.name ?? 'No project registered'}</span>
        <span className={styles.pathText}>{project?.path ?? 'Choose a local directory to begin'}</span>
        <span className={styles.localBadge}><i /> Session active</span>
      </div>
      <div className={styles.pageHeading}>
        <div><span className={styles.kicker}>AGENT MANAGER / {String(navItems.findIndex((item) => item.id === view) + 1).padStart(2, '0')}</span><h1>{pageTitle}</h1></div>
        {project && view === 'workspace' && <button className={styles.secondaryButton} type="button" onClick={() => void loadWorkspace(project.id)} disabled={workspace.loading}>↻ <span>Refresh</span></button>}
      </div>

      {message && <div className={`${styles.notice} ${message.kind === 'error' ? styles.noticeError : styles.noticeSuccess}`} role={message.kind === 'error' ? 'alert' : 'status'}>{message.text}<button type="button" aria-label="Dismiss message" onClick={() => setMessage(null)}>×</button></div>}

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
      </>}
      <footer className={styles.pageFooter}><span>AGENT MANAGER</span><span>LOCAL SESSION · NO NETWORK CONTENT</span></footer>
    </main>
  </div>
}

function ProjectRegistration({ path, setPath, onSubmit, busy }: { path: string; setPath: (path: string) => void; onSubmit: (event: React.FormEvent<HTMLFormElement>) => void; busy: boolean }) {
  return <section className={styles.registerPanel}>
    <div className={styles.registerIndex}>01 <span>/ PROJECT</span></div>
    <div className={styles.registerCopy}><span className={styles.kicker}>START WITH A PROJECT</span><h2>Choose one local workspace.</h2><p>The console inspects this directory through Agent Manager’s existing read-only services. It does not expose general file browsing.</p></div>
    <form className={styles.registerForm} onSubmit={onSubmit}>
      <label htmlFor="project-path">Project directory</label>
      <div className={styles.inputAction}><input id="project-path" value={path} onChange={(event) => setPath(event.target.value)} placeholder="/Users/you/work/my-project" autoComplete="off" required /><button className={styles.primaryButton} type="submit" disabled={busy}>{busy ? 'Checking…' : 'Inspect project'}</button></div>
      <p>Paste an absolute path or a path relative to where the console was started.</p>
    </form>
    <div className={styles.registerStamp}><span>01</span><span>SESSION<br />BOUND</span></div>
  </section>
}

function Workspace({ page, onActivate, onRemove, onDiagnostics }: {
  page: PageState<{ agents: Agent[]; recommendations: RecommendationResult; resources: Resource[] }>
  onActivate: (skill: Skill) => void
  onRemove: (resource: Resource) => void
  onDiagnostics: () => void
}) {
  if (page.loading && !page.data) return <LoadingState label="Reading this project…" />
  if (page.error) return <ErrorState message={page.error} />
  if (!page.data) return <EmptyState title="Workspace is ready" detail="Refresh to read the project inventory." />
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
      <div className={styles.panelTop}><span className={styles.kicker}>PROJECT SNAPSHOT</span><span className={styles.statusDot}><i /> Read only</span></div>
      <h2>{technologies.length ? `${technologies.length} signals found` : 'No stack signals yet'}</h2>
      <p>{recommendations.scanComplete ? 'Static markers were read from the project tree.' : 'The scan reached its traversal limit; results are partial.'}</p>
      <div className={styles.technologyRow}>{technologies.length ? technologies.map((item) => <span className={styles.techChip} key={`${item.scope}-${item.id}`}>{item.label}<small>{item.scope}</small>{item.evidence.map((evidencePath) => <small key={evidencePath}>Evidence · {evidencePath}</small>)}</span>) : <span className={styles.mutedText}>Add a supported project marker to see evidence here.</span>}</div>
      {!recommendations.scanComplete && <div className={styles.inlineWarning}>Partial scan · review scan diagnostics</div>}
    </section>

    <section className={`${styles.panel} ${styles.agentPanel}`}>
      <div className={styles.panelTop}><span className={styles.kicker}>AGENT ADAPTERS</span><span className={styles.countMark}>{agents.length.toString().padStart(2, '0')}</span></div>
      <div className={styles.agentList}>{agents.map((agent) => <div className={styles.agentRow} key={agent.id}>
        <span className={`${styles.agentGlyph} ${agent.status === 'unsupported' ? styles.agentGlyphMuted : ''}`}>{agent.id.slice(0, 1).toUpperCase()}</span>
        <div className={styles.agentInfo}><strong>{humanize(agent.id)}</strong><span>{agent.capabilities.length ? agent.capabilities.join(' · ') : agent.status}</span></div>
        <span className={`${styles.pill} ${agent.availability === 'configured' ? styles.pillReady : agent.status === 'unsupported' ? styles.pillMuted : styles.pillQuiet}`}>{humanize(agent.availability)}</span>
      </div>)}</div>
      <p className={styles.panelFoot}>{unavailable} adapter{unavailable === 1 ? '' : 's'} need attention or are unsupported.</p>
    </section>

    <section className={styles.panel}>
      <div className={styles.panelTop}><span className={styles.kicker}>SKILL RECOMMENDATIONS</span><span className={styles.countMark}>{recommended.length.toString().padStart(2, '0')}</span></div>
      {recommended.length ? <div className={styles.recommendationList}>{recommended.map((item, index) => <article className={styles.recommendationRow} key={`${item.scope}-${item.identifier}`}>
        <span className={styles.findingIndex}>{String(index + 1).padStart(2, '0')}</span>
        <div><strong>{item.identifier}</strong><p>{item.description}</p><span className={styles.reasonText}>{item.matchReason}</span></div>
        <div className={styles.recommendationAction}><span className={styles.pill}>{item.confidence} · {item.score}</span><button type="button" className={styles.textButton} onClick={() => onActivate({ identifier: item.identifier, name: item.identifier, description: item.description, tags: [], compatibility: [] })}>Review activation →</button></div>
      </article>)}</div> : <EmptyInline title="No matching Skills" detail="Search the eligible library to browse available Skills." />}
    </section>

    <section className={`${styles.panel} ${styles.inventoryPanel}`}>
      <div className={styles.panelTop}><span className={styles.kicker}>PROJECT INVENTORY</span><span className={styles.countMark}>{resources.length.toString().padStart(2, '0')}</span></div>
      {resources.length ? <div className={styles.inventoryList}>{resources.map((resource) => <div className={styles.inventoryRow} key={`${resource.target}-${resource.identifier}`}>
        <div className={styles.resourceMain}><span className={styles.pill}>{humanize(resource.status)}</span><strong>{resource.identifier}</strong><span className={styles.mutedText}>{humanize(resource.target)}</span><code>{resource.path}</code></div>
        {resource.status === 'managed' && <button className={styles.subtleDangerButton} type="button" onClick={() => onRemove(resource)}>Plan removal</button>}
      </div>)}</div> : <EmptyInline title="No project Skills" detail="This project has no entries in supported Skill locations." />}
    </section>

    <section className={`${styles.panel} ${styles.diagnosticsStrip}`}>
      <div><span className={styles.kicker}>DIAGNOSTICS</span><strong>{recommendations.diagnostics.length} scan notes</strong><span>Doctor findings are available in a read-only view.</span></div>
      <button className={styles.secondaryButton} type="button" onClick={onDiagnostics}>Open diagnostics <span>↗</span></button>
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
  return <div className={styles.libraryLayout}>
    <section className={`${styles.panel} ${styles.libraryPanel}`}>
      <div className={styles.panelTop}><span className={styles.kicker}>ELIGIBLE LIBRARY</span><span className={styles.countMark}>{page.data?.length.toString().padStart(2, '0') ?? '—'}</span></div>
      <form className={styles.searchForm} onSubmit={onSearch}><label className={styles.srOnly} htmlFor="skill-search">Search Skills</label><input id="skill-search" type="search" placeholder="Find by name, tag, or description" value={query} onChange={(event) => setQuery(event.target.value)} /><button className={styles.secondaryButton} type="submit">Search</button></form>
      {page.loading ? <LoadingState label="Searching eligible Skills…" /> : page.error ? <ErrorState message={page.error} /> : page.data?.length ? <div className={styles.skillList}>{page.data.map((skill) => <button type="button" className={`${styles.skillRow} ${selected?.identifier === skill.identifier ? styles.skillRowActive : ''}`} key={skill.identifier} onClick={() => onSelect(skill)}>
        <span className={styles.skillBullet}>↗</span><span className={styles.skillSummary}><strong>{skill.name || skill.identifier}</strong><span>{skill.description}</span><small>{skill.tags.length ? skill.tags.join(' · ') : 'No tags'}</small></span><span className={styles.chevron}>›</span>
      </button>)}</div> : <EmptyInline title="No eligible Skills found" detail="Try another term or add a valid SKILL.md to the configured library." />}
    </section>
    <section className={`${styles.panel} ${styles.skillDetailPanel}`}>
      {selected ? <>
        <div className={styles.panelTop}><span className={styles.kicker}>SKILL DETAIL / {selected.identifier}</span><span className={styles.pill}>{selected.provenance || 'Local library'}</span></div>
        <h2>{selected.name || selected.identifier}</h2><p className={styles.detailLead}>{selected.description}</p>
        <div className={styles.metadataRow}><span>Tags <strong>{selected.tags.join(', ') || 'None'}</strong></span><span>Compatibility <strong>{selected.compatibility.join(', ') || 'Not declared'}</strong></span></div>
        <div className={styles.documentLabel}>SKILL.MD CONTENT <span>READ ONLY · NEVER EXECUTED</span></div>
        <pre className={styles.skillBody}>{selected.body ?? 'Select a Skill to read its content.'}</pre>
        <fieldset className={styles.targetFieldset}><legend>Activation targets</legend><div className={styles.targetChecks}>{['claude-code', 'codex', 'pi'].map((target) => <label key={target}><input type="checkbox" checked={targets.includes(target)} onChange={() => onToggleTarget(target)} /><span>{humanize(target)}</span></label>)}</div></fieldset>
        <label className={styles.forceCheck}><input type="checkbox" checked={forceReplace} onChange={(event) => setForceReplace(event.target.checked)} /><span>Allow replacement of existing conflicts</span><small>Requires this separate confirmation and is recorded in the review plan.</small></label>
        <button className={styles.primaryButton} type="button" disabled={busy || !targets.length} onClick={() => onActivate(selected)}>{busy ? 'Preparing preview…' : 'Review activation plan'}</button>
      </> : <EmptyState title="Select a Skill" detail="Open a catalog entry to inspect its metadata and plan a guarded activation." />}
    </section>
  </div>
}

function SubAgentsPage({ page }: { page: PageState<{ definitions: SubAgent[]; diagnostics: SubAgentDiagnostic[] }> }) {
  if (page.loading && !page.data) return <LoadingState label="Reading canonical definitions…" />
  if (page.error) return <ErrorState message={page.error} />
  if (!page.data) return <EmptyState title="SubAgents are unavailable" detail="Try refreshing this view." />
  return <div className={styles.documentGrid}>
    <section className={styles.panel}><div className={styles.panelTop}><span className={styles.kicker}>CANONICAL DEFINITIONS</span><span className={styles.countMark}>{page.data.definitions.length.toString().padStart(2, '0')}</span></div>
      {page.data.definitions.length ? page.data.definitions.map((definition) => <article className={styles.definitionCard} key={definition.id}><div className={styles.panelTop}><h2>{definition.name}</h2><span className={styles.pill}>v{definition.version}</span></div><p className={styles.roleLine}>{definition.id} · {definition.role}</p><p className={styles.instructions}>{definition.instructions}</p><div className={styles.metadataRow}><span>Skills <strong>{definition.skills?.join(', ') || 'None'}</strong></span><span>Agents <strong>{definition.compatibility?.agents?.join(', ') || 'Any'}</strong></span><span>Capabilities <strong>{definition.requiredCapabilities?.join(', ') || 'None'}</strong></span></div></article>) : <EmptyInline title="No canonical SubAgents" detail="Definitions are read from the configured Agent Manager data root." />}
    </section>
    <section className={styles.panel}><div className={styles.panelTop}><span className={styles.kicker}>VALIDATION NOTES</span><span className={styles.countMark}>{page.data.diagnostics.length.toString().padStart(2, '0')}</span></div>
      {page.data.diagnostics.length ? page.data.diagnostics.map((finding) => <div className={styles.findingRow} key={`${finding.path}-${finding.message}`}><strong>{finding.id || 'Invalid definition'}</strong><p>{finding.message}</p><code>{finding.path}</code></div>) : <EmptyInline title="All definitions are valid" detail="SubAgent inspection is read only; this view has no installation controls." />}
    </section>
  </div>
}

function DiagnosticsPage({ page, onRefresh }: { page: PageState<Finding[]>; onRefresh: () => void }) {
  return <section className={styles.panel}><div className={styles.panelTop}><span className={styles.kicker}>READ-ONLY DOCTOR</span><button className={styles.secondaryButton} type="button" onClick={onRefresh}>↻ <span>Run scan</span></button></div>
    <p className={styles.detailLead}>Findings come from the existing doctor checks. This scan never reconciles project state.</p>
    {page.loading ? <LoadingState label="Scanning configured resources…" /> : page.error ? <ErrorState message={page.error} /> : page.data?.length ? page.data.map((finding, index) => <div className={styles.findingRow} key={`${finding.path}-${index}`}><span className={styles.findingIndex}>{String(index + 1).padStart(2, '0')}</span><div><strong>{finding.message}</strong><code>{finding.path}</code></div></div>) : <EmptyInline title="No findings" detail="The current read-only doctor scan returned no issues." />}
  </section>
}

function OperationsPage({ page, onUndo, busy }: { page: PageState<LatestOperation>; onUndo: () => void; busy: boolean }) {
  if (page.loading && page.data === null) return <LoadingState label="Reading operation journal…" />
  if (page.error) return <ErrorState message={page.error} />
  return <section className={styles.panel}><div className={styles.panelTop}><span className={styles.kicker}>LATEST REVERSIBLE OPERATION</span><span className={styles.pill}>PROJECT JOURNAL</span></div>
    {page.data ? <div className={styles.operationCard}><span className={styles.operationGlyph}>↶</span><div><span className={styles.kicker}>{humanize(page.data.resourceKind)} · {new Date(page.data.at).toLocaleString()}</span><h2>{page.data.operation}</h2><p>{page.data.undoPreview?.changes.length ?? 0} paths in the available Undo preview.</p></div>{page.data.undoAvailable && <button className={styles.secondaryButton} type="button" disabled={busy} onClick={onUndo}>Review Undo</button>}</div> : <EmptyInline title="No operation journal yet" detail="Successful guarded operations appear here with an Undo preview." />}
  </section>
}

function PlanReview({ plan, busy, onExecute, onCancel }: { plan: OperationPlan; busy: boolean; onExecute: () => void; onCancel: () => void }) {
  return <section className={styles.reviewPanel} aria-labelledby="review-title">
    <div className={styles.reviewTop}><div><span className={styles.kicker}>REVIEW BEFORE APPLYING</span><h2 id="review-title">{plan.operation}</h2></div><span className={styles.reviewExpiry}>Expires {new Date(plan.expiresAt).toLocaleTimeString()}</span></div>
    {plan.forceReplacementConfirmed && <div className={styles.forceNotice}>Force replacement confirmed for this plan. Existing conflicting content may be replaced.</div>}
    <ol className={styles.changeList}>{plan.changes.map((change, index) => <li key={`${change.path}-${index}`}><span className={styles.changeNumber}>{String(index + 1).padStart(2, '0')}</span><div><strong>{change.action}</strong>{change.detail && <span>{change.detail}</span>}<code>{change.path}</code></div></li>)}</ol>
    {plan.warnings.map((warning) => <p className={styles.inlineWarning} key={warning}>{warning}</p>)}
    <div className={styles.reviewActions}><button className={styles.primaryButton} type="button" disabled={busy} onClick={onExecute}>{busy ? 'Applying…' : 'Confirm and apply'}</button><button className={styles.secondaryButton} type="button" disabled={busy} onClick={onCancel}>Cancel plan</button></div>
  </section>
}

function LoadingState({ label }: { label: string }) { return <div className={styles.stateBox} role="status"><span className={styles.loaderMark} />{label}</div> }
function ErrorState({ message }: { message: string }) { return <div className={`${styles.stateBox} ${styles.errorState}`} role="alert"><strong>Could not load this view</strong><span>{message}</span></div> }
function EmptyState({ title, detail }: { title: string; detail: string }) { return <div className={styles.stateBox}><strong>{title}</strong><span>{detail}</span></div> }
function EmptyInline({ title, detail }: { title: string; detail: string }) { return <div className={styles.emptyInline}><strong>{title}</strong><span>{detail}</span></div> }
function humanize(value: string) { return value.replaceAll('-', ' ').replace(/\b\w/g, (letter) => letter.toUpperCase()) }
function errorMessage(error: unknown) { return error instanceof APIError ? error.message : error instanceof Error ? error.message : 'An unexpected local service error occurred.' }

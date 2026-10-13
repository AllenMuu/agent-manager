import React, { useEffect, useMemo, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { request as apiRequest } from './api.js'
import { initialLanguage, presentMessage, saveLanguage, targetLabel, translate } from './i18n.js'
import { previewPayload } from './selection.js'
import './style.css'

const session = document.querySelector('meta[name="agent-manager-session"]')?.content ?? ''
const request = (path, options = {}) => apiRequest(path, options, session)

function App() {
  const [language, setLanguage] = useState(initialLanguage)
  const [query, setQuery] = useState('')
  const [skills, setSkills] = useState([])
  const [targets, setTargets] = useState([])
  const [selected, setSelected] = useState([])
  const [targetChoices, setTargetChoices] = useState([])
  const [recommendation, setRecommendation] = useState(null)
  const [preview, setPreview] = useState(null)
  const [replaceConflicts, setReplaceConflicts] = useState(false)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  const t = (key) => translate(key, language)

  useEffect(() => {
    document.documentElement.lang = language
    document.title = language === 'zh-CN' ? 'Agent Manager · 安装技能' : 'Agent Manager · Install Skills'
  }, [language])

  function changeLanguage(nextLanguage) {
    setLanguage(nextLanguage)
    saveLanguage(nextLanguage)
  }

  useEffect(() => {
    let alive = true
    const timer = window.setTimeout(async () => {
      const { response, body } = await request(`/api/catalog?q=${encodeURIComponent(query)}`)
      if (alive && response.ok) {
        setSkills(body.skills ?? [])
        setTargetChoices(body.targets ?? [])
      } else if (alive) {
        setError(body.error ?? 'Could not read the local Skill library.')
      }
    }, 120)
    return () => {
      alive = false
      window.clearTimeout(timer)
    }
  }, [query])

  useEffect(() => {
    let alive = true
    request('/api/recommend').then(({ response, body }) => {
      if (!alive) return
      if (response.ok) setRecommendation(body)
      else setError(body.error ?? 'Could not scan this project.')
    })
    return () => { alive = false }
  }, [])

  const selectedSkills = useMemo(() => new Set(selected), [selected])
  const recommended = useMemo(() => {
    const byID = new Map()
    for (const scope of recommendation?.scopes ?? []) {
      for (const item of scope.recommendations ?? []) byID.set(item.identifier, item)
    }
    return [...byID.values()]
  }, [recommendation])

  function toggleSkill(id) {
    setSelected((current) => current.includes(id) ? current.filter((item) => item !== id) : [...current, id])
    setPreview(null)
    setMessage('')
  }

  function toggleTarget(id) {
    setTargets((current) => current.includes(id) ? current.filter((item) => item !== id) : [...current, id])
    setPreview(null)
    setMessage('')
  }

  async function createPreview(replacement = replaceConflicts, notice = '') {
    setBusy(true)
    setError('')
    setMessage(notice)
    setPreview(null)
    try {
      const payload = previewPayload(selected, targets, replacement)
      const { response, body } = await request('/api/install/preview', { method: 'POST', body: JSON.stringify(payload) })
      if (body.preview) setPreview(body.preview)
      if (!response.ok) setError(body.error ?? 'Could not create an installation preview.')
      return response.ok
    } catch {
      setError('Could not create an installation preview. Check that the local service is running and retry.')
      return false
    } finally {
      setBusy(false)
    }
  }

  async function confirmInstall() {
    if (!preview?.id) return
    setBusy(true)
    setError('')
    setMessage('')
    try {
      const { response, body } = await request('/api/install/apply', {
        method: 'POST',
        body: JSON.stringify({ previewId: preview.id }),
      })
      if (response.status === 409 && body.stale) {
        setPreview(null)
        await createPreview(replaceConflicts, 'The project changed after preview. Review this updated plan, then confirm again.')
        return
      }
      if (!response.ok) {
        setError(body.error ?? 'Installation could not be completed.')
        return
      }
      setMessage('Selected Skills installed successfully. The operation is recorded and can be undone with the CLI.')
      setPreview(null)
    } catch {
      setError('Installation could not be completed. Check that the local service is running and retry.')
    } finally {
      setBusy(false)
    }
  }

  const planHasConflict = (preview?.plan?.changes ?? []).some((change) => change.action.includes('conflict'))

  return (
    <main className="shell">
      <header className="topbar">
        <a className="brand" href="#top" aria-label={t('home')}>
          <span className="brand-mark">A</span>
          <span>Agent Manager</span>
        </a>
        <div className="topbar-actions">
          <div className="language-switch" role="group" aria-label={t('language')}>
            <button type="button" onClick={() => changeLanguage('en')} aria-label={t('english')} aria-pressed={language === 'en'}>EN</button>
            <button type="button" onClick={() => changeLanguage('zh-CN')} aria-label={t('chinese')} aria-pressed={language === 'zh-CN'}>中文</button>
          </div>
          <span className="local-indicator"><i /> {t('localProject')}</span>
        </div>
      </header>

      <section className="intro" id="top">
        <div className="eyebrow">{t('skillLibrary')} <span>·</span> {t('projectActivation')}</div>
        <h1>{t('headline')}</h1>
        <p>{t('intro')}</p>
        <div className="project-line"><span className="project-icon">⌂</span><span>{recommendation?.project ?? t('readingProject')}</span></div>
      </section>

      {error && <div className="notice notice-error" role="alert"><span>!</span>{presentMessage(error, language)}<button onClick={() => setError('')} aria-label={t('dismiss')}>×</button></div>}
      {message && <div className="notice notice-success" role="status"><span>✓</span>{presentMessage(message, language)}</div>}

      <section className="recommend-section">
        <div className="section-heading">
          <div><div className="eyebrow">{t('projectMatches')}</div><h2>{t('recommended')}</h2></div>
          <span className="quiet-label">{t('readOnly')}</span>
        </div>
        {recommended.length ? <div className="recommend-grid">
          {recommended.slice(0, 4).map((item) => <SkillCard key={item.identifier} item={item} selected={selectedSkills.has(item.identifier)} onToggle={toggleSkill} recommended language={language} />)}
        </div> : <div className="empty-recommend">{recommendation ? t('noMatches') : t('scanning')}</div>}
      </section>

      <section className="library-section">
        <div className="section-heading library-heading">
          <div><div className="eyebrow">{t('localLibrary')}</div><h2>{t('browseSkills')}</h2></div>
          <label className="search-box"><span>⌕</span><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t('search')} /></label>
        </div>
        <div className="skill-list">
          {skills.length ? skills.map((item) => <SkillCard key={item.identifier} item={item} selected={selectedSkills.has(item.identifier)} onToggle={toggleSkill} language={language} />) : <div className="empty-list">{t('noSkills')}</div>}
        </div>
      </section>

      <section className="install-panel">
        <div className="install-panel-top">
          <div><div className="eyebrow">{t('installSelection')}</div><h2>{t('chooseTargets')}</h2></div>
          <span className="selection-count">{selected.length} {t('selected')}</span>
        </div>
        <div className="target-list">
          {targetChoices.map((target) => <label className={`target-option ${targets.includes(target.id) ? 'checked' : ''}`} key={target.id}>
            <input type="checkbox" checked={targets.includes(target.id)} onChange={() => toggleTarget(target.id)} />
            <span className="checkbox-mark">✓</span><span>{targetLabel(target.id, target.label, language)}</span>
          </label>)}
        </div>
        <div className="action-row">
          <div className="action-hint">{selected.length === 0 ? t('selectSkill') : targets.length === 0 ? t('selectTarget') : t('reviewChanges')}</div>
          <button className="primary-button" disabled={busy || selected.length === 0 || targets.length === 0} onClick={() => createPreview()}>
            {busy ? t('working') : t('previewInstall')} <span aria-hidden="true">→</span>
          </button>
        </div>
      </section>

      {preview && <section className="preview-panel" aria-live="polite">
        <div className="preview-heading"><div><div className="eyebrow">{t('reviewBeforeInstall')}</div><h2>{presentMessage(preview.plan.operation, language)}</h2></div><span className="plan-count">{preview.plan.changes.length} {t('fileChanges')}</span></div>
        <ul className="change-list">{preview.plan.changes.map((change, index) => <li key={`${change.path}-${index}`}><span className={change.action.includes('conflict') ? 'change-icon conflict' : 'change-icon'}>{change.action.includes('conflict') ? '!' : '↗'}</span><span className="change-copy"><strong>{presentMessage(change.action, language)}</strong><code>{change.path}</code></span></li>)}</ul>
        {(preview.plan.warnings ?? []).map((warning) => <div className="warning-line" key={warning}><span>△</span>{presentMessage(warning, language)}</div>)}
        {planHasConflict && !replaceConflicts && <label className="force-option"><input type="checkbox" checked={replaceConflicts} onChange={(event) => { setReplaceConflicts(event.target.checked); setPreview(null) }} /><span>{t('allowReplacement')}</span><small>{t('forceRecorded')}</small></label>}
        {planHasConflict && replaceConflicts && <div className="force-active"><span>!</span> {t('replacementEnabled')}</div>}
        <div className="preview-footer"><span>{t('nothingUntilConfirm')}</span>
          {planHasConflict && !replaceConflicts ? <button className="secondary-button" onClick={() => { setReplaceConflicts(true); createPreview(true, 'Replacement preview requested. Review the affected paths before confirming.') }} disabled={busy}>{t('previewReplacement')}</button> : <button className="primary-button" onClick={confirmInstall} disabled={busy || !preview.id}>{busy ? t('applying') : t('confirmInstall')} <span>✓</span></button>}
        </div>
      </section>}

      <footer><span>Agent Manager</span><span>{t('runsLocally')}</span></footer>
    </main>
  )
}

function SkillCard({ item, selected, onToggle, recommended = false, language }) {
  return <label className={`skill-card ${selected ? 'selected' : ''}`}>
    <input type="checkbox" checked={selected} onChange={() => onToggle(item.identifier)} />
    <span className="card-check">✓</span>
    <span className="skill-copy"><span className="skill-title-row"><strong>{item.name || item.identifier}</strong><code>{item.identifier}</code>{recommended && <span className="match-badge">{translate('match', language)}</span>}</span><span className="skill-description">{item.description}</span>
      {item.matchReason && <span className="match-reason">{presentMessage(item.matchReason, language)}</span>}
      {item.tags?.length > 0 && <span className="tag-row">{item.tags.slice(0, 4).map((tag) => <span key={tag}>{tag}</span>)}</span>}
    </span>
    <span className="compatibility">{item.compatibility?.length ? item.compatibility.join(' · ') : translate('compatibilityUndeclared', language)}</span>
  </label>
}

createRoot(document.getElementById('root')).render(<React.StrictMode><App /></React.StrictMode>)

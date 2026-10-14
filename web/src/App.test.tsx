import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'
import { LanguageProvider } from './i18n'
import { sessionStorageKey } from './session'
import type { OperationPlan, Project, Skill } from './types'

afterEach(() => {
  cleanup()
  window.sessionStorage.clear()
  window.localStorage.clear()
  window.history.replaceState(null, '', '/')
  vi.unstubAllGlobals()
})

describe('console entry and guarded activation', () => {
  it('shows the entry guidance when no tab token is available', () => {
    renderApp()
    expect(screen.getByRole('heading', { name: 'Open the console entry link' })).toBeInTheDocument()
  })

  it('shows marker evidence and handles SubAgents without compatibility agents', async () => {
    const project: Project = { id: 'project-session-id', name: 'sample', path: '/workspace/sample' }
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const route = String(input)
      if (route === '/api/v1/system') return jsonResponse({ project })
      if (route.endsWith('/agents')) return jsonResponse({ agents: [] })
      if (route.endsWith('/recommendations')) return jsonResponse({
        project: project.path,
        scanComplete: true,
        diagnostics: [],
        scopes: [{
          path: project.path,
          status: 'detected',
          technologies: [{ id: 'go', label: 'Go', category: 'language' }],
          evidence: [{ path: 'go.mod', technology: 'go' }],
          diagnostics: [],
          recommendations: Array.from({ length: 6 }, (_, index) => ({
            identifier: `skill-${index + 1}`,
            description: `Recommendation ${index + 1}`,
            matchReason: 'matches detected evidence',
            confidence: 'high',
            score: 100 - index,
          })),
          nextAction: 'none',
        }],
      })
      if (route.endsWith('/resources')) return jsonResponse({ resources: [] })
      if (route === '/api/v1/subagents') return jsonResponse({
        definitions: [{ version: 'v1', id: 'reviewer', name: 'Reviewer', role: 'Review code', instructions: 'Review carefully.', compatibility: {} }],
        diagnostics: [],
      })
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')

    renderApp()
    expect(await screen.findByText(/Evidence · go\.mod/)).toBeInTheDocument()
    expect(screen.getByText('skill-6')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /SubAgents/ }))
    expect(await screen.findByRole('heading', { name: 'Reviewer', level: 2 })).toBeInTheDocument()
    expect(screen.getByText('Any')).toBeInTheDocument()
  })

  it('unifies ordered recommendations, catalog selection, search, and one detail panel', async () => {
    const project: Project = { id: 'project-session-id', name: 'sample', path: '/workspace/sample' }
    const skill: Skill = { identifier: 'shared', name: 'Shared Skill', description: 'Catalog shared entry', tags: ['go'], compatibility: ['codex'], body: 'Shared detail content.' }
    const recommendations = {
      project: project.path, scanComplete: true, diagnostics: [],
      scopes: ['z', 'a'].map((scope, scopeIndex) => ({
        path: `${project.path}/${scope}`, status: 'detected', technologies: [], evidence: [], diagnostics: [], nextAction: 'none',
        recommendations: Array.from({ length: scopeIndex === 0 ? 2 : 5 }, (_, index) => ({
          identifier: scopeIndex === 0 && index === 0 ? 'shared' : `${scope}-${index}`, description: `${scope} item ${index}`,
          matchReason: 'fallback reason', matchReasonRef: { key: 'recommendation.matched_evidence' }, matchingEvidence: [{ technology: 'go', field: 'tags' }], confidence: 'high', score: index + 3,
        })),
      })),
    }
    const calls: string[] = []
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const route = String(input)
      calls.push(route)
      if (route === '/api/v1/system') return jsonResponse({ project })
      if (route.endsWith('/agents')) return jsonResponse({ agents: [] })
      if (route.endsWith('/resources')) return jsonResponse({ resources: [] })
      if (route.endsWith('/recommendations')) return jsonResponse(recommendations)
      if (route === '/api/catalog?q=') return jsonResponse({ skills: [skill], targets: [{ id: 'codex', label: 'Codex' }] })
      if (route === '/api/catalog?q=other') return jsonResponse({ skills: [], targets: [{ id: 'codex', label: 'Codex' }] })
      if (route === '/api/v1/skills/shared') return jsonResponse({ skill })
      return jsonResponse({})
    }))
    window.history.replaceState(null, '', '/#session=tab-secret')
    renderApp()
    await screen.findByText('sample')
    fireEvent.click(screen.getByRole('button', { name: /Skills/ }))
    const recommended = await screen.findByRole('region', { name: 'Recommended Skills' })
    const catalog = screen.getByRole('region', { name: 'Eligible library' })
    await within(recommended).findByRole('checkbox', { name: 'Select shared' })
    expect(recommended.compareDocumentPosition(catalog) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(within(recommended).getAllByRole('checkbox').map((item) => item.getAttribute('aria-label'))).toEqual(['Select shared', 'Select z-1', 'Select a-0', 'Select a-1', 'Select a-2', 'Select a-3', 'Select a-4'])
    expect(within(recommended).getAllByText('Matched project evidence: go · tags')).toHaveLength(7)
    expect(within(recommended).getAllByText('High · 3')).toHaveLength(2)
    expect(within(recommended).getByText('/workspace/sample/z')).toBeInTheDocument()
    expect(within(recommended).getByText('/workspace/sample/a')).toBeInTheDocument()
    expect(screen.getByRole('searchbox', { name: 'Search Skills' })).toBeInTheDocument()
    expect(screen.getAllByRole('region', { name: 'Skill detail' })).toHaveLength(1)
    expect(screen.getByText('0 selected')).toBeInTheDocument()
    const recommendationSelection = within(recommended).getByRole('checkbox', { name: 'Select shared' })
    const catalogSelection = await within(catalog).findByRole('checkbox', { name: 'Select shared' })
    fireEvent.click(recommendationSelection)
    expect(catalogSelection).toBeChecked()
    expect(screen.getByText('1 selected')).toBeInTheDocument()
    fireEvent.click(catalogSelection)
    expect(recommendationSelection).not.toBeChecked()
    expect(screen.getByText('0 selected')).toBeInTheDocument()
    fireEvent.click(within(recommended).getAllByRole('button', { name: 'Read details →' })[0])
    expect(await screen.findByText('Shared detail content.')).toBeInTheDocument()
    expect(screen.getByText('0 selected')).toBeInTheDocument()
    fireEvent.click(within(catalog).getByRole('button', { name: /Shared Skill/ }))
    expect(await screen.findByText('Shared detail content.')).toBeInTheDocument()
    expect(screen.getByText('0 selected')).toBeInTheDocument()
    fireEvent.click(recommendationSelection)
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search Skills' }), { target: { value: 'other' } })
    fireEvent.click(screen.getByRole('button', { name: 'Search' }))
    await waitFor(() => expect(within(catalog).queryByRole('checkbox')).not.toBeInTheDocument())
    expect(screen.getByText('1 selected')).toBeInTheDocument()
    expect(recommendationSelection).toBeChecked()
    expect(calls).toContain('/api/catalog?q=other')
    expect(calls.some((route) => route.includes('/plans/') || route.includes('/api/install'))).toBe(false)
    expect(screen.queryByRole('button', { name: /Skill installation/ })).not.toBeInTheDocument()
    expect(document.querySelector('iframe')).toBeNull()
  })

  it('keeps the catalog available when the recommendation service fails', async () => {
    const skill: Skill = { identifier: 'local', name: 'Local Skill', description: 'Available locally', tags: [], compatibility: [] }
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const route = String(input)
      if (route === '/api/v1/system') return jsonResponse({ project: { id: 'project-session-id', name: 'sample', path: '/workspace/sample' } })
      if (route.endsWith('/recommendations')) return jsonResponse({ error: { code: 'scan_failed', message: 'Recommendation scan failed.' } }, 500)
      if (route === '/api/catalog?q=') return jsonResponse({ skills: [skill], targets: [] })
      return jsonResponse({})
    }))
    window.history.replaceState(null, '', '/install/#session=tab-secret')
    renderApp()
    expect(await screen.findByRole('button', { name: /Local Skill/ })).toBeInTheDocument()
    const recommended = screen.getByRole('region', { name: 'Recommended Skills' })
    expect(await within(recommended).findByRole('alert')).toHaveTextContent('Recommendation scan failed.')
  })

  it('opens Skills for the compatibility installation pathname', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const route = String(input)
      if (route === '/api/v1/system') return jsonResponse({ project: { id: 'project-session-id', name: 'sample', path: '/workspace/sample' } })
      if (route.endsWith('/recommendations')) return jsonResponse({ scopes: [], scanComplete: true, diagnostics: [] })
      if (route === '/api/catalog?q=') return jsonResponse({ skills: [], targets: [] })
      return jsonResponse({})
    }))
    window.history.replaceState(null, '', '/install/#session=tab-secret')
    renderApp()
    expect(screen.getByRole('button', { name: /Skills/ })).toHaveAttribute('aria-current', 'page')
    expect(await screen.findByRole('searchbox', { name: 'Search Skills' })).toBeInTheDocument()
  })

  it('bootstraps the token, reads a Skill, reviews its plan, and confirms through the local API', async () => {
    const project: Project = { id: 'project-session-id', name: 'sample', path: '/workspace/sample' }
    const skill: Skill = { identifier: 'go-helper', name: 'Go Helper', description: 'Helps with Go projects', tags: ['go'], compatibility: ['codex'], body: 'Use Go tooling.' }
    const plan: OperationPlan = {
      id: 'opaque-plan-id', projectId: project.id, operation: 'activate selected skills', version: 'v1', resourceKind: 'skill',
      changes: [{ path: '/workspace/sample/.codex/skills/go-helper', action: 'create absolute link', detail: '/library/go-helper' }],
      warnings: [], createdAt: '2026-09-27T00:00:00Z', expiresAt: '2026-09-27T00:10:00Z', forceReplacementConfirmed: false,
    }
    const calls: { route: string; init: RequestInit }[] = []
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
      const route = String(input)
      calls.push({ route, init })
      let payload: unknown = {}
      if (route === '/api/v1/system') payload = { project }
      else if (route.endsWith('/agents')) payload = { agents: [] }
      else if (route.endsWith('/recommendations')) payload = { project: project.path, scanComplete: true, diagnostics: [], scopes: [] }
      else if (route.endsWith('/resources')) payload = { resources: [] }
      else if (route === '/api/catalog?q=') payload = { skills: [{ ...skill, body: undefined }], targets: [{ id: 'codex', label: 'Codex' }] }
      else if (route === '/api/v1/skills/go-helper') payload = { skill }
      else if (route.endsWith('/plans/activate')) payload = { plan }
      else if (route.endsWith('/execute')) payload = { operation: plan.operation, status: 'completed' }
      return new Response(JSON.stringify(payload), { status: 200, headers: { 'Content-Type': 'application/json' } })
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')

    renderApp()
    expect(await screen.findByText('sample')).toBeInTheDocument()
    expect(window.sessionStorage.getItem(sessionStorageKey)).toBe('tab-secret')
    expect(window.location.hash).toBe('')

    fireEvent.click(screen.getByRole('button', { name: /Skills/ }))
    fireEvent.click(await screen.findByRole('button', { name: /Go Helper/ }))
    expect(await screen.findByText('Use Go tooling.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Review activation plan' }))
    expect(await screen.findByRole('heading', { name: 'activate selected skills', level: 2 })).toBeInTheDocument()
    expect(screen.getByText('/workspace/sample/.codex/skills/go-helper')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Confirm and apply' }))
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'activate selected skills', level: 2 })).not.toBeInTheDocument())
    const planCall = calls.find((call) => call.route.endsWith('/plans/activate'))
    expect(planCall).toBeDefined()
    expect(new Headers(planCall?.init.headers).get('Authorization')).toBe('Bearer tab-secret')
    expect(planCall?.init.body).toBe(JSON.stringify({ skillIdentifiers: ['go-helper'], targetAgents: ['codex'] }))
    expect(calls.some((call) => call.route.includes('tab-secret'))).toBe(false)
    expect(calls.some((call) => call.route.endsWith('/opaque-plan-id/execute'))).toBe(true)
  })

  it('drops a stale review and asks for a fresh preview after the API rejects execution', async () => {
    const project: Project = { id: 'project-session-id', name: 'sample', path: '/workspace/sample' }
    const skill: Skill = { identifier: 'go-helper', name: 'Go Helper', description: 'Helps with Go projects', tags: [], compatibility: [], body: 'Use Go tooling.' }
    const plan: OperationPlan = {
      id: 'stale-plan-id', projectId: project.id, operation: 'activate selected skills', version: 'v1', resourceKind: 'skill',
      changes: [{ path: '/workspace/sample/.codex/skills/go-helper', action: 'create absolute link' }],
      warnings: [], createdAt: '2026-09-27T00:00:00Z', expiresAt: '2026-09-27T00:10:00Z', forceReplacementConfirmed: false,
    }
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const route = String(input)
      if (route === '/api/v1/system') return jsonResponse({ project })
      if (route.endsWith('/agents')) return jsonResponse({ agents: [] })
      if (route.endsWith('/recommendations')) return jsonResponse({ project: project.path, scanComplete: true, diagnostics: [], scopes: [] })
      if (route.endsWith('/resources')) return jsonResponse({ resources: [] })
      if (route === '/api/catalog?q=') return jsonResponse({ skills: [{ ...skill, body: undefined }], targets: [{ id: 'codex', label: 'Codex' }] })
      if (route === '/api/v1/skills/go-helper') return jsonResponse({ skill })
      if (route.endsWith('/plans/activate')) return jsonResponse({ plan })
      if (route.endsWith('/execute')) return jsonResponse({ error: { code: 'stale_plan', message: 'Project state changed. Create a fresh preview.' } }, 409)
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')

    renderApp()
    await screen.findByText('sample')
    fireEvent.click(screen.getByRole('button', { name: /Skills/ }))
    fireEvent.click(await screen.findByRole('button', { name: /Go Helper/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Review activation plan' }))
    await screen.findByRole('heading', { name: 'activate selected skills', level: 2 })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm and apply' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Project state changed. Create a fresh preview.')
    expect(screen.queryByRole('heading', { name: 'activate selected skills', level: 2 })).not.toBeInTheDocument()
  })

  it('closes a terminal execution error and refreshes project state after the journal was committed', async () => {
    const project: Project = { id: 'project-session-id', name: 'sample', path: '/workspace/sample' }
    const skill: Skill = { identifier: 'go-helper', name: 'Go Helper', description: 'Helps with Go projects', tags: [], compatibility: [], body: 'Use Go tooling.' }
    const plan: OperationPlan = {
      id: 'committed-plan-id', projectId: project.id, operation: 'activate selected skills', version: 'v1', resourceKind: 'skill',
      changes: [{ path: '/workspace/sample/.codex/skills/go-helper', action: 'create absolute link' }],
      warnings: [], createdAt: '2026-09-27T00:00:00Z', expiresAt: '2026-09-27T00:10:00Z', forceReplacementConfirmed: false,
    }
    const calls: string[] = []
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const route = String(input)
      calls.push(route)
      if (route === '/api/v1/system') return jsonResponse({ project })
      if (route.endsWith('/agents')) return jsonResponse({ agents: [] })
      if (route.endsWith('/recommendations')) return jsonResponse({ project: project.path, scanComplete: true, diagnostics: [], scopes: [] })
      if (route.endsWith('/resources')) return jsonResponse({ resources: [] })
      if (route === '/api/catalog?q=') return jsonResponse({ skills: [{ ...skill, body: undefined }], targets: [{ id: 'codex', label: 'Codex' }] })
      if (route === '/api/v1/skills/go-helper') return jsonResponse({ skill })
      if (route.endsWith('/plans/activate')) return jsonResponse({ plan })
      if (route.endsWith('/execute')) return jsonResponse({ error: { code: 'journal_committed', message: 'The operation was applied and its journal record was published.' } }, 500)
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')

    renderApp()
    await screen.findByText('sample')
    fireEvent.click(screen.getByRole('button', { name: /Skills/ }))
    fireEvent.click(await screen.findByRole('button', { name: /Go Helper/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Review activation plan' }))
    await screen.findByRole('heading', { name: 'activate selected skills', level: 2 })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm and apply' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('The operation was applied and its journal record was published.')
    expect(screen.queryByRole('heading', { name: 'activate selected skills', level: 2 })).not.toBeInTheDocument()
    await waitFor(() => expect(calls.filter((route) => route.endsWith('/resources')).length).toBeGreaterThan(1))
  })
  it('localizes detected adapters and orphaned resources in Chinese', async () => {
    const project: Project = { id: 'project-session-id', name: 'sample', path: '/workspace/sample' }
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const route = String(input)
      if (route === '/api/v1/system') return jsonResponse({ project })
      if (route.endsWith('/agents')) return jsonResponse({ agents: [{ id: 'codex', status: 'configured', availability: 'detected', resourceKinds: ['skill'], capabilities: [] }] })
      if (route.endsWith('/recommendations')) return jsonResponse({ project: project.path, scanComplete: true, diagnostics: [], scopes: [] })
      if (route.endsWith('/resources')) return jsonResponse({ resources: [{ target: 'codex', identifier: 'stale-skill', path: '/workspace/sample/.codex/skills/stale-skill', status: 'orphaned' }] })
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')

    renderApp()
    expect(await screen.findByText('sample')).toBeInTheDocument()
    expect(await screen.findByText('stale-skill')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Simplified Chinese' }))

    expect(await screen.findByText('已检测')).toBeInTheDocument()
    expect(screen.getByText('来源缺失')).toBeInTheDocument()
  })

  it('localizes undo plan headings and completion notifications in Chinese', async () => {
    const project: Project = { id: 'project-session-id', name: 'sample', path: '/workspace/sample' }
    const undoRef = { key: 'plan.operation.undo_action', args: { operation: 'plan.operation.activate' } }
    const undoPlan: OperationPlan = {
      id: 'undo-plan-id', projectId: project.id, operation: 'undo activate', operationRef: undoRef, version: 'v1', resourceKind: 'skill',
      changes: [{ path: '/workspace/sample/.codex/skills/go-helper', action: 'restore pre-operation state' }],
      warnings: [], createdAt: '2026-09-27T00:00:00Z', expiresAt: '2026-09-27T00:10:00Z', forceReplacementConfirmed: false,
    }
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const route = String(input)
      if (route === '/api/v1/system') return jsonResponse({ project })
      if (route.endsWith('/agents')) return jsonResponse({ agents: [] })
      if (route.endsWith('/recommendations')) return jsonResponse({ project: project.path, scanComplete: true, diagnostics: [], scopes: [] })
      if (route.endsWith('/resources')) return jsonResponse({ resources: [] })
      if (route.endsWith('/operations/latest')) return jsonResponse({ latest: { operation: 'activate', operationRef: { key: 'plan.operation.activate' }, resourceKind: 'skill', at: '2026-09-27T00:00:00Z', undoAvailable: true } })
      if (route.endsWith('/plans/undo')) return jsonResponse({ plan: undoPlan })
      if (route.endsWith('/execute')) return jsonResponse({ operation: 'undo activate', status: 'completed' })
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')

    renderApp()
    expect(await screen.findByText('sample')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Simplified Chinese' }))
    fireEvent.click(screen.getByRole('button', { name: /操作记录/ }))
    fireEvent.click(await screen.findByRole('button', { name: '审查撤销方案' }))

    expect(await screen.findByRole('heading', { name: '撤销启用', level: 2 })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认并应用' }))

    expect(await screen.findByRole('status')).toHaveTextContent('已完成 撤销启用，并写入操作日志。')
  })
})

function renderApp() {
  return render(<LanguageProvider><App /></LanguageProvider>)
}

function jsonResponse(payload: unknown, status = 200) {
  return new Response(JSON.stringify(payload), { status, headers: { 'Content-Type': 'application/json' } })
}

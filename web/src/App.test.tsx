import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'
import { sessionStorageKey } from './session'
import type { OperationPlan, Project, Skill } from './types'

afterEach(() => {
  cleanup()
  window.sessionStorage.clear()
  window.history.replaceState(null, '', '/')
  vi.unstubAllGlobals()
})

describe('console entry and guarded activation', () => {
  it('shows the entry guidance when no tab token is available', () => {
    render(<App />)
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

    render(<App />)
    expect(await screen.findByText(/Evidence · go\.mod/)).toBeInTheDocument()
    expect(screen.getByText('skill-6')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /SubAgents/ }))
    expect(await screen.findByRole('heading', { name: 'Reviewer', level: 2 })).toBeInTheDocument()
    expect(screen.getByText('Any')).toBeInTheDocument()
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
      else if (route === '/api/v1/skills?q=') payload = { skills: [{ ...skill, body: undefined }] }
      else if (route === '/api/v1/skills/go-helper') payload = { skill }
      else if (route.endsWith('/plans/activate')) payload = { plan }
      else if (route.endsWith('/execute')) payload = { operation: plan.operation, status: 'completed' }
      return new Response(JSON.stringify(payload), { status: 200, headers: { 'Content-Type': 'application/json' } })
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')

    render(<App />)
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
      if (route === '/api/v1/skills?q=') return jsonResponse({ skills: [{ ...skill, body: undefined }] })
      if (route === '/api/v1/skills/go-helper') return jsonResponse({ skill })
      if (route.endsWith('/plans/activate')) return jsonResponse({ plan })
      if (route.endsWith('/execute')) return jsonResponse({ error: { code: 'stale_plan', message: 'Project state changed. Create a fresh preview.' } }, 409)
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')

    render(<App />)
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
      if (route === '/api/v1/skills?q=') return jsonResponse({ skills: [{ ...skill, body: undefined }] })
      if (route === '/api/v1/skills/go-helper') return jsonResponse({ skill })
      if (route.endsWith('/plans/activate')) return jsonResponse({ plan })
      if (route.endsWith('/execute')) return jsonResponse({ error: { code: 'journal_committed', message: 'The operation was applied and its journal record was published.' } }, 500)
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')

    render(<App />)
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
})

function jsonResponse(payload: unknown, status = 200) {
  return new Response(JSON.stringify(payload), { status, headers: { 'Content-Type': 'application/json' } })
}

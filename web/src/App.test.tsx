import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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

  it('keeps the narrow-screen reading order as recommendations, catalog, inline details, then selection actions', async () => {
    vi.stubGlobal('innerWidth', 375)
    installFixture()
    renderApp()
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Select go-helper' }))
    fireEvent.click(screen.getByRole('button', { name: /Go Helper/ }))
    const recommendations = screen.getByRole('region', { name: 'Recommended Skills' })
    const catalog = screen.getByRole('region', { name: 'Eligible library' })
    const details = screen.getByRole('region', { name: 'Skill detail' })
    const actions = screen.getByRole('region', { name: 'Selected Skills' })
    for (const [before, after] of [[recommendations, catalog], [catalog, details], [details, actions]]) {
      expect(before.compareDocumentPosition(after) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    }
    const review = within(actions).getByRole('button', { name: 'Review activation plan' })
    review.focus()
    expect(review).toHaveFocus()
    expect(within(actions).getByRole('button', { name: 'Deselect go-helper' })).not.toHaveAttribute('tabindex', '-1')
  })

  it('translates unified workspace actions while preserving identifiers, paths, and content', async () => {
    installFixture((route) => route.startsWith('/api/v1/skills/') ? jsonResponse({ skill: { ...operationSkills[0], body: 'Untranslated SKILL.md content.' } }) : undefined)
    renderApp()
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Select go-helper' }))
    fireEvent.click(screen.getByRole('button', { name: /Go Helper/ }))
    await screen.findByText('Untranslated SKILL.md content.')
    fireEvent.click(screen.getByRole('button', { name: 'Simplified Chinese' }))
    expect(screen.getByRole('region', { name: '推荐 Skill' })).toBeInTheDocument()
    expect(screen.getByText('已选择 1 项')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '取消选择 go-helper' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '审查启用方案' })).toBeInTheDocument()
    expect(screen.getByText('Untranslated SKILL.md content.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: '选择 go-test' }))
    expect(screen.getByRole('group', { name: '安装目标' })).toBeInTheDocument()
    fireEvent.click(within(screen.getByRole('group', { name: '安装目标' })).getByRole('checkbox', { name: 'pi' }))
    fireEvent.click(screen.getByRole('button', { name: '预览安装' }))
    expect(await screen.findByRole('heading', { name: '审查安装' })).toBeInTheDocument()
    expect(screen.getByText('/workspace/sample/.pi/skills/go-helper')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认安装' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '取消安装' }))
    expect(screen.getByText('已选择 2 项')).toBeInTheDocument()
  })

  it('keeps unknown installation API errors unchanged in Chinese', async () => {
    installFixture((route) => route === '/api/install/preview' ? jsonResponse({ error: { code: 'unknown_install_error', message: 'Unknown local error: /workspace/sample/go-helper' } }, 422) : undefined)
    renderApp()
    await selectBatch()
    fireEvent.click(screen.getByRole('button', { name: 'Simplified Chinese' }))
    fireEvent.click(screen.getByRole('button', { name: '预览安装' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Unknown local error: /workspace/sample/go-helper')
  })

  it('shows a blocked installation preview from a string error without allowing apply', async () => {
    const { calls } = installFixture((route) => route === '/api/install/preview' ? jsonResponse({
      error: 'destination is not a managed Skill link: /workspace/sample/.pi/skills/go-helper',
      preview: {
        skillIds: ['go-helper', 'go-test'],
        targets: ['pi'],
        plan: {
          operation: 'activate',
          changes: [{ path: '/workspace/sample/.pi/skills/go-helper', action: 'refuse conflicting destination' }],
          warnings: [],
        },
      },
    }, 409) : undefined)
    renderApp()
    await selectBatch()
    fireEvent.click(screen.getByRole('button', { name: 'Preview installation' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('destination is not a managed Skill link: /workspace/sample/.pi/skills/go-helper')
    expect(await screen.findByRole('heading', { name: 'Review installation' })).toBeInTheDocument()
    expect(screen.getByText('/workspace/sample/.pi/skills/go-helper')).toBeInTheDocument()
    const confirm = screen.getByRole('button', { name: 'Confirm installation' })
    expect(confirm).toBeDisabled()
    fireEvent.click(confirm)
    expect(calls.filter((call) => call.route === '/api/install/apply')).toHaveLength(0)
  })

  it('translates known legacy installation API error strings in Chinese', async () => {
    installFixture((route) => route === '/api/install/preview' ? jsonResponse({ error: 'invalid installation request' }, 422) : undefined)
    renderApp()
    await selectBatch()
    fireEvent.click(screen.getByRole('button', { name: 'Simplified Chinese' }))
    fireEvent.click(screen.getByRole('button', { name: '预览安装' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('安装请求无效')
  })

  it('translates legacy batch plan actions and warnings while preserving unknown text and paths', async () => {
    installFixture((route) => route === '/api/install/preview' ? jsonResponse({ preview: {
      id: 'localized-preview',
      skillIds: ['go-helper', 'go-test'],
      targets: ['pi'],
      plan: {
        operation: 'activate',
        changes: [
          { path: '/workspace/sample/.pi/skills/go-helper', action: 'create absolute link' },
          { path: '/workspace/sample/.pi/skills/go-test', action: 'replace conflicting path with absolute link' },
          { path: '/workspace/sample/.pi/skills/go-review', action: 'refuse conflicting destination' },
          { path: '/workspace/sample/.pi/skills/go-check', action: 'replace conflict (force required)' },
          { path: '/workspace/sample/.pi/skills/custom', action: 'future action: preserve /workspace/sample/custom' },
        ],
        warnings: [
          'resource is already installed; no changes made',
          'go-helper is not declared compatible with pi',
          'future warning: inspect /workspace/sample/.pi/skills/custom',
        ],
      },
    } }) : undefined)
    renderApp()
    await selectBatch()
    fireEvent.click(screen.getByRole('button', { name: 'Simplified Chinese' }))
    fireEvent.click(screen.getByRole('button', { name: '预览安装' }))

    await screen.findByRole('heading', { name: '审查安装' })
    expect(screen.getByText('创建绝对路径链接')).toBeInTheDocument()
    expect(screen.getByText('用绝对路径链接替换冲突路径')).toBeInTheDocument()
    expect(screen.getByText('拒绝替换冲突目标')).toBeInTheDocument()
    expect(screen.getByText('替换冲突项（需要强制确认）')).toBeInTheDocument()
    expect(screen.getByText('future action: preserve /workspace/sample/custom')).toBeInTheDocument()
    expect(screen.getByText('资源已安装；未进行更改')).toBeInTheDocument()
    expect(screen.getByText('技能“go-helper”未声明与 Pi 兼容')).toBeInTheDocument()
    expect(screen.getByText('future warning: inspect /workspace/sample/.pi/skills/custom')).toBeInTheDocument()
    expect(screen.getByText('/workspace/sample/.pi/skills/go-helper')).toBeInTheDocument()
  })

  it('uses selected count for guarded actions and explicit batch targets', async () => {
    const { calls } = installFixture()
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: /Go Helper/ }))
    expect(screen.queryByRole('button', { name: 'Review activation plan' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Preview installation' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select go-helper' }))
    fireEvent.click(screen.getByRole('button', { name: /Go Test/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Review activation plan' }))
    await screen.findByRole('heading', { name: 'activate selected skills' })
    expect(JSON.parse(calls.find((call) => call.route.endsWith('/plans/activate'))!.init.body as string)).toEqual({ skillIdentifiers: ['go-helper'], targetAgents: ['codex'] })
    fireEvent.click(screen.getByRole('button', { name: 'Cancel plan' }))
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'activate selected skills' })).not.toBeInTheDocument())
    expect(screen.getByText('1 selected')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select go-test' }))
    expect(screen.queryByRole('button', { name: 'Review activation plan' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Preview installation' })).toBeDisabled()
    const targets = screen.getByRole('group', { name: 'Installation targets' })
    expect(within(targets).getByRole('checkbox', { name: 'codex' })).not.toBeChecked()
    fireEvent.click(within(targets).getByRole('checkbox', { name: 'pi' }))
    await previewBatch()
    expect(JSON.parse(calls.find((call) => call.route === '/api/install/preview')!.init.body as string)).toEqual({ skillIds: ['go-helper', 'go-test'], targets: ['pi'], replaceConflicts: false })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm installation' }))
    await waitFor(() => expect(screen.getByText('0 selected')).toBeInTheDocument())
    expect(JSON.parse(calls.find((call) => call.route === '/api/install/apply')!.init.body as string)).toEqual({ previewId: 'preview-1' })
    expect(calls.some((call) => call.route.endsWith('/resources'))).toBe(true)
    expect(calls.some((call) => call.route.endsWith('/operations/latest'))).toBe(true)
  })

  it.each(['selection', 'targets', 'conflicts'])('removes a batch preview when %s changes', async (input) => {
    installFixture()
    renderApp()
    await selectBatch()
    await previewBatch()
    if (input === 'selection') fireEvent.click(screen.getByRole('checkbox', { name: 'Select go-test' }))
    if (input === 'targets') fireEvent.click(within(screen.getByRole('group', { name: 'Installation targets' })).getByRole('checkbox', { name: 'codex' }))
    if (input === 'conflicts') fireEvent.click(screen.getByRole('checkbox', { name: /Allow replacement of existing conflicts/ }))
    expect(screen.queryByRole('heading', { name: 'Review installation' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Confirm installation' })).not.toBeInTheDocument()
  })

  it('requests a fresh preview after stale batch apply and requires a second confirmation', async () => {
    let applies = 0
    const { calls } = installFixture((route) => {
      if (route === '/api/install/apply' && ++applies === 1) return jsonResponse({ stale: true, result: { stale: true, applied: false } }, 409)
    })
    renderApp()
    await selectBatch()
    await previewBatch()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm installation' }))
    await waitFor(() => expect(calls.filter((call) => call.route === '/api/install/preview')).toHaveLength(2))
    expect(await screen.findByText('The project changed after preview. Review this updated plan, then confirm again.')).toBeInTheDocument()
    expect(applies).toBe(1)
    expect(screen.getByText('2 selected')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm installation' }))
    await waitFor(() => expect(screen.getByText('0 selected')).toBeInTheDocument())
    expect(JSON.parse(calls.filter((call) => call.route === '/api/install/apply')[1].init.body as string)).toEqual({ previewId: 'preview-2' })
  })

  it('retains a blocked preview returned with a string error after stale batch apply', async () => {
    let previews = 0
    let applies = 0
    const { calls } = installFixture((route) => {
      if (route === '/api/install/preview' && ++previews === 2) return jsonResponse({
        error: 'destination is not a managed Skill link: /workspace/sample/.pi/skills/go-helper',
        preview: {
          skillIds: ['go-helper', 'go-test'],
          targets: ['pi'],
          plan: {
            operation: 'activate',
            changes: [{ path: '/workspace/sample/.pi/skills/go-helper', action: 'refuse conflicting destination' }],
            warnings: [],
          },
        },
      }, 409)
      if (route === '/api/install/apply') {
        applies++
        return jsonResponse({ stale: true, result: { stale: true, applied: false } }, 409)
      }
    })
    renderApp()
    await selectBatch()
    await previewBatch()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm installation' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('destination is not a managed Skill link: /workspace/sample/.pi/skills/go-helper')
    expect(await screen.findByRole('heading', { name: 'Review installation' })).toBeInTheDocument()
    expect(screen.getByText('/workspace/sample/.pi/skills/go-helper')).toBeInTheDocument()
    const confirm = screen.getByRole('button', { name: 'Confirm installation' })
    expect(confirm).toBeDisabled()
    fireEvent.click(confirm)
    expect(applies).toBe(1)
    expect(calls.filter((call) => call.route === '/api/install/preview')).toHaveLength(2)
    expect(screen.getByText('2 selected')).toBeInTheDocument()
  })

  it('retains batch selection on cancellation and failed apply', async () => {
    installFixture((route) => route === '/api/install/apply' ? jsonResponse({ error: { code: 'apply_failed', message: 'Apply failed.' } }, 500) : undefined)
    renderApp()
    await selectBatch()
    await previewBatch()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel installation' }))
    expect(screen.getByText('2 selected')).toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'Review installation' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Preview installation' }))
    await screen.findByRole('heading', { name: 'Review installation' })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm installation' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Apply failed.')
    expect(screen.getByText('2 selected')).toBeInTheDocument()
  })

  it('retries only recommendations while keeping catalog browsing usable', async () => {
    let attempts = 0
    const { calls } = installFixture((route) => route.endsWith('/recommendations') && ++attempts === 1 ? jsonResponse({ error: { code: 'scan_failed', message: 'Scan failed.' } }, 500) : undefined)
    renderApp()
    expect(await screen.findByRole('button', { name: /Go Helper/ })).toBeInTheDocument()
    const recommendations = screen.getByRole('region', { name: 'Recommended Skills' })
    expect(await within(recommendations).findByRole('alert')).toHaveTextContent('Scan failed.')
    fireEvent.click(within(recommendations).getByRole('button', { name: 'Retry recommendations' }))
    await waitFor(() => expect(within(recommendations).queryByRole('alert')).not.toBeInTheDocument())
    expect(attempts).toBe(2)
    expect(calls.filter((call) => call.route === '/api/catalog?q=')).toHaveLength(1)
  })

  it('blocks mutations on catalog failure without removing selection', async () => {
    let fail = false
    installFixture((route) => fail && route.startsWith('/api/catalog') ? jsonResponse({ error: { code: 'catalog_failed', message: 'Catalog failed.' } }, 500) : undefined)
    renderApp()
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Select go-helper' }))
    fail = true
    fireEvent.click(screen.getByRole('button', { name: 'Search' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Catalog failed.')
    expect(screen.getByText('1 selected')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Review activation plan' })).toBeDisabled()
  })

  it('checks availability independently of search and keeps missing selections visible until deselected', async () => {
    let remove = false
    const { calls } = installFixture((route) => {
      if (route === '/api/catalog?q=other') return jsonResponse({ skills: [], targets: supportedTargets })
      if (route === '/api/catalog?q=' && remove) return jsonResponse({ skills: [operationSkills[0]], targets: supportedTargets })
    })
    renderApp()
    await selectBatch()
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search Skills' }), { target: { value: 'other' } })
    fireEvent.click(screen.getByRole('button', { name: 'Search' }))
    await screen.findByText('No eligible Skills found')
    expect(screen.queryByText('go-test · Unavailable')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Preview installation' })).toBeEnabled()
    remove = true
    fireEvent.click(screen.getByRole('button', { name: 'Search' }))
    expect(await screen.findByText('go-test · Unavailable')).toBeInTheDocument()
    expect(screen.getByText('2 selected')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Preview installation' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Deselect go-helper' }))
    expect(screen.getByRole('button', { name: 'Review activation plan' })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Deselect go-test' }))
    expect(screen.getByText('0 selected')).toBeInTheDocument()
    expect(calls.filter((call) => call.route === '/api/catalog?q=')).toHaveLength(3)
  })

  it('renders installation previews with null Go slice fields', async () => {
    installFixture((route) => route === '/api/install/preview' ? jsonResponse({ preview: { id: 'null-slices', skillIds: ['go-helper', 'go-test'], targets: ['pi'], plan: { operation: 'activate', changes: null, warnings: null } } }) : undefined)
    renderApp()
    await selectBatch()
    await previewBatch()
    expect(screen.getByRole('button', { name: 'Confirm installation' })).toBeEnabled()
  })

  it('blocks batch mutation when only the unfiltered catalog fails', async () => {
    let fail = false
    installFixture((route) => {
      if (route === '/api/catalog?q=other') return jsonResponse({ skills: [], targets: supportedTargets })
      if (route === '/api/catalog?q=' && fail) return jsonResponse({ error: { code: 'catalog_failed', message: 'Full catalog failed.' } }, 500)
    })
    renderApp()
    await selectBatch()
    fail = true
    fireEvent.change(screen.getByRole('searchbox', { name: 'Search Skills' }), { target: { value: 'other' } })
    fireEvent.click(screen.getByRole('button', { name: 'Search' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Full catalog failed.')
    expect(screen.getByRole('button', { name: 'Preview installation' })).toBeDisabled()
    expect(screen.getByText('2 selected')).toBeInTheDocument()
  })

  it('keeps batch selection when stale preview regeneration fails', async () => {
    let previews = 0
    installFixture((route) => {
      if (route === '/api/install/apply') return jsonResponse({ stale: true }, 409)
      if (route === '/api/install/preview' && ++previews === 2) return jsonResponse({ error: { code: 'preview_failed', message: 'Fresh preview failed.' } }, 500)
    })
    renderApp()
    await selectBatch()
    await previewBatch()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm installation' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Fresh preview failed.')
    expect(screen.getByText('2 selected')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Confirm installation' })).not.toBeInTheDocument()
  })

  it('preserves single activation conflict confirmation and independent target choices', async () => {
    const { calls } = installFixture()
    renderApp()
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Select go-helper' }))
    fireEvent.click(screen.getByRole('checkbox', { name: /Allow replacement of existing conflicts/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Review activation plan' }))
    await screen.findByRole('heading', { name: 'activate selected skills' })
    expect(JSON.parse(calls.find((call) => call.route.endsWith('/plans/activate'))!.init.body as string)).toEqual({ skillIdentifiers: ['go-helper'], targetAgents: ['codex'], conflictStrategy: 'replace', forceConfirmed: true })
    fireEvent.click(screen.getByRole('button', { name: 'Cancel plan' }))
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'activate selected skills' })).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select go-test' }))
    const batchTargets = screen.getByRole('group', { name: 'Installation targets' })
    expect(within(batchTargets).getByRole('checkbox', { name: 'codex' })).not.toBeChecked()
    expect(screen.getByRole('checkbox', { name: /Allow replacement of existing conflicts/ })).not.toBeChecked()
    fireEvent.click(within(batchTargets).getByRole('checkbox', { name: 'pi' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select go-test' }))
    const singleTargets = screen.getByRole('group', { name: 'Activation targets' })
    expect(within(singleTargets).getByRole('checkbox', { name: 'codex' })).toBeChecked()
    expect(within(singleTargets).getByRole('checkbox', { name: 'pi' })).not.toBeChecked()
    expect(screen.getByRole('checkbox', { name: /Allow replacement of existing conflicts/ })).toBeChecked()
  })

  it('preserves Skill selection when an Undo review replaces an activation review', async () => {
    installFixture((route) => {
      if (route.endsWith('/operations/latest')) return jsonResponse({ latest: { operation: 'activate', resourceKind: 'skill', at: '2026-10-15T00:00:00Z', undoAvailable: true } })
      if (route.endsWith('/plans/undo')) return jsonResponse({ plan: { id: 'undo-plan', operation: 'undo activate', resourceKind: 'skill', changes: [], warnings: [], expiresAt: '2026-10-15T00:10:00Z' } })
      if (route.endsWith('/execute')) return jsonResponse({ operation: 'undo activate', status: 'completed' })
    })
    renderApp()
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Select go-helper' }))
    fireEvent.click(screen.getByRole('button', { name: 'Review activation plan' }))
    await screen.findByRole('heading', { name: 'activate selected skills' })
    fireEvent.click(screen.getByRole('button', { name: /Operations/ }))
    fireEvent.click(await screen.findByRole('button', { name: 'Review Undo' }))
    await screen.findByRole('heading', { name: 'undo activate' })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm and apply' }))
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'undo activate' })).not.toBeInTheDocument())
    fireEvent.click(screen.getByRole('button', { name: /Skills/ }))
    expect(screen.getByText('1 selected')).toBeInTheDocument()
  })

  it('blocks overlapping removal while a deferred batch preview crosses navigation', async () => {
    const delayedPreview = deferred<Response>()
    const thirdSkill: Skill = { identifier: 'go-review', name: 'Go Review', description: 'Go review', tags: [], compatibility: [] }
    let previews = 0
    const { calls } = installFixture((route, init) => {
      if (route === '/api/catalog?q=') return jsonResponse({ skills: [...operationSkills, thirdSkill], targets: supportedTargets })
      if (route.endsWith('/resources')) return jsonResponse({ resources: [{ target: 'codex', identifier: 'installed', path: '/workspace/sample/.codex/skills/installed', status: 'managed' }] })
      if (route.endsWith('/plans/remove')) return jsonResponse({ plan: { id: 'removal-plan', operation: 'remove selected skill', changes: [], warnings: [], expiresAt: '2026-10-15T00:10:00Z' } })
      if (route === '/api/install/preview') {
        if (++previews === 1) return delayedPreview.promise
        const payload = JSON.parse(init.body as string)
        return jsonResponse({ preview: { id: 'current-preview', skillIds: payload.skillIds, targets: payload.targets, plan: { operation: 'activate', changes: [], warnings: [] } } })
      }
    })
    renderApp()
    await selectBatch()
    fireEvent.click(screen.getByRole('button', { name: 'Preview installation' }))
    await waitFor(() => expect(calls.filter((call) => call.route === '/api/install/preview')).toHaveLength(1))
    fireEvent.click(screen.getByRole('button', { name: /Workspace/ }))
    const remove = await screen.findByRole('button', { name: 'Plan removal' })
    expect(remove).toBeDisabled()
    fireEvent.click(remove)
    expect(calls.some((call) => call.route.endsWith('/plans/remove'))).toBe(false)
    fireEvent.click(screen.getByRole('button', { name: /Skills/ }))
    expect(screen.getByRole('checkbox', { name: 'Select go-test' })).toBeDisabled()
    await act(async () => delayedPreview.resolve(jsonResponse({ preview: { id: 'old-preview', skillIds: ['go-helper', 'go-test'], targets: ['pi'], plan: { operation: 'activate', changes: [], warnings: [] } } })))
    expect(screen.queryByRole('button', { name: 'Confirm installation' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select go-test' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select go-review' }))
    await previewBatch()
    expect(JSON.parse(calls.filter((call) => call.route === '/api/install/preview')[1].init.body as string)).toEqual({ skillIds: ['go-helper', 'go-review'], targets: ['pi'], replaceConflicts: false })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm installation' }))
    await waitFor(() => expect(screen.getByText('0 selected')).toBeInTheDocument())
    expect(calls.filter((call) => call.route === '/api/install/apply').map((call) => JSON.parse(call.init.body as string))).toEqual([{ previewId: 'current-preview' }])
  })

  it('discards a deferred preview response after leaving Skills invalidates its review', async () => {
    const delayedPreview = deferred<Response>()
    installFixture((route) => route === '/api/install/preview' ? delayedPreview.promise : undefined)
    renderApp()
    await selectBatch()
    fireEvent.click(screen.getByRole('button', { name: 'Preview installation' }))
    fireEvent.click(screen.getByRole('button', { name: /Workspace/ }))
    await screen.findByText('No project Skills')
    fireEvent.click(screen.getByRole('button', { name: /Skills/ }))
    await act(async () => delayedPreview.resolve(jsonResponse({ preview: { id: 'late-preview', skillIds: ['go-helper', 'go-test'], targets: ['pi'], plan: { operation: 'activate', changes: [], warnings: [] } } })))
    expect(screen.queryByRole('heading', { name: 'Review installation' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Preview installation' })).toBeEnabled()
    expect(screen.getByText('2 selected')).toBeInTheDocument()
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
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select go-helper' }))
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
    expect(screen.getByText('0 selected')).toBeInTheDocument()
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
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select go-helper' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Review activation plan' }))
    await screen.findByRole('heading', { name: 'activate selected skills', level: 2 })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm and apply' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Project state changed. Create a fresh preview.')
    expect(screen.getByText('1 selected')).toBeInTheDocument()
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
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select go-helper' }))
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

const supportedTargets = [{ id: 'codex', label: 'codex' }, { id: 'pi', label: 'pi' }]
const operationSkills: Skill[] = [
  { identifier: 'go-helper', name: 'Go Helper', description: 'Go help', tags: [], compatibility: [] },
  { identifier: 'go-test', name: 'Go Test', description: 'Go tests', tags: [], compatibility: [] },
]

function installFixture(override?: (route: string, init: RequestInit) => Response | Promise<Response> | undefined) {
  const calls: { route: string; init: RequestInit }[] = []
  let previews = 0
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const route = String(input)
    calls.push({ route, init })
    const response = override?.(route, init)
    if (response) return response
    if (route === '/api/v1/system') return jsonResponse({ project: { id: 'project-session-id', name: 'sample', path: '/workspace/sample' } })
    if (route === '/api/catalog?q=') return jsonResponse({ skills: operationSkills, targets: supportedTargets })
    if (route.endsWith('/recommendations')) return jsonResponse({ project: '/workspace/sample', scanComplete: true, diagnostics: [], scopes: [] })
    if (route.endsWith('/agents')) return jsonResponse({ agents: [] })
    if (route.endsWith('/resources')) return jsonResponse({ resources: [] })
    if (route.endsWith('/operations/latest')) return jsonResponse({ latest: null })
    if (route.startsWith('/api/v1/skills/')) return jsonResponse({ skill: operationSkills.find((skill) => route.endsWith(skill.identifier)) })
    if (route.endsWith('/plans/activate')) return jsonResponse({ plan: { id: 'single-plan', operation: 'activate selected skills', resourceKind: 'skill', changes: [], warnings: [], expiresAt: '2026-10-15T00:10:00Z' } })
    if (route === '/api/install/preview') return jsonResponse({ preview: { id: `preview-${++previews}`, skillIds: ['go-helper', 'go-test'], targets: ['pi'], plan: { operation: 'activate', changes: [{ path: '/workspace/sample/.pi/skills/go-helper', action: 'create link' }], warnings: [] } } })
    if (route === '/api/install/apply') return jsonResponse({ result: { applied: true, stale: false } })
    return jsonResponse({})
  }))
  window.history.replaceState(null, '', '/install/#session=tab-secret')
  return { calls }
}

async function selectBatch() {
  fireEvent.click(await screen.findByRole('checkbox', { name: 'Select go-helper' }))
  fireEvent.click(screen.getByRole('checkbox', { name: 'Select go-test' }))
  fireEvent.click(within(screen.getByRole('group', { name: 'Installation targets' })).getByRole('checkbox', { name: 'pi' }))
}

async function previewBatch() {
  fireEvent.click(screen.getByRole('button', { name: 'Preview installation' }))
  await screen.findByRole('heading', { name: 'Review installation' })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((fulfill) => { resolve = fulfill })
  return { promise, resolve }
}

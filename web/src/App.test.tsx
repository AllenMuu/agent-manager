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

describe('console entry and guarded installation', () => {
  it('shows the entry guidance when no tab token is available', () => {
    renderApp()
    expect(screen.getByRole('heading', { name: 'Open the console entry link' })).toBeInTheDocument()
  })

  it('imports a global Skill and reviews a scoped resync without a registered project', async () => {
    const calls: { route: string; init: RequestInit }[] = []
    const source = {
      id: 'source-global-demo', identifier: 'demo-skill', scope: 'global', location: 'Claude Code',
      path: '/home/user/.claude/skills/demo-skill', checksum: 'sha256-demo', eligible: true,
    }
    const group = {
      id: 'group-demo', identifier: 'demo-skill', checksum: 'sha256-demo', versionKey: 'v-demo',
      sources: [source], alreadyPresent: false, conflict: false, makeCurrent: true,
    }
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
      const route = String(input)
      calls.push({ route, init })
      if (route === '/api/v1/system') return jsonResponse({})
      if (route === '/api/v1/sync/settings' && init.method === 'PUT') return jsonResponse({ repositoryPath: '/home/user/.agent-manager/skills', syncMode: 'copy-only' })
      if (route === '/api/v1/sync/settings') return jsonResponse({ repositoryPath: '/home/user/.agent-manager/skills', syncMode: 'auto' })
      if (route === '/api/v1/sync/sources') return jsonResponse({
        repositoryPath: '/home/user/.agent-manager/skills', locations: [], sources: [source], groups: [group], diagnostics: [],
      })
      if (route === '/api/v1/sync/import/preview') return jsonResponse({ planId: 'import-plan', preview: {
        repositoryPath: '/home/user/.agent-manager/skills', groups: [group], selectedGroupIds: ['group-demo'],
        repositoryStates: {}, confirmationToken: 'reviewed-import',
      } }, 201)
      if (route === '/api/v1/sync/import/apply') return jsonResponse({ imported: 1, entries: [] })
      if (route === '/api/v1/sync/resync/preview') return jsonResponse({ planId: 'resync-plan', scope: 'global', plan: {
        id: 'resync-plan', operation: 'resynchronize managed Skills', changes: [{ path: '/home/user/.codex/skills/demo-skill', action: 'copy Skill tree' }], warnings: [],
      } }, 201)
      if (route === '/api/v1/sync/resync/apply') return jsonResponse({ operation: 'resynchronize managed Skills', changes: 1 })
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')

    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: /Storage & Sync/ }))
    expect(await screen.findByRole('heading', { name: 'Storage & Sync', level: 2 })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Global' })).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'Project' })).toBeInTheDocument()
    expect(screen.getByRole('radio', { name: /Automatic/ })).toBeChecked()
    fireEvent.click(screen.getByRole('button', { name: 'Simplified Chinese' }))
    expect(await screen.findByRole('heading', { name: '存储与同步', level: 2 })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'English' }))

    fireEvent.click(screen.getByRole('radio', { name: /Copy only/ }))
    expect(screen.getByRole('button', { name: 'Save sync mode' })).toBeEnabled()
    expect(calls.some((call) => call.route === '/api/v1/sync/settings' && call.init.method === 'PUT')).toBe(false)
    fireEvent.click(screen.getByRole('button', { name: 'Save sync mode' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Sync mode saved.')

    fireEvent.click(screen.getByRole('checkbox', { name: 'Select demo-skill' }))
    fireEvent.click(screen.getByRole('button', { name: 'Review selected import' }))
    expect(await screen.findByRole('heading', { name: 'Review before importing', level: 2 })).toBeInTheDocument()
    expect(screen.getAllByText(/\/home\/user\/\.claude\/skills\/demo-skill/).length).toBeGreaterThan(0)
    expect(screen.getByRole('checkbox', { name: 'Select demo-skill' })).toBeDisabled()
    expect(screen.getByRole('radio', { name: /Copy only/ })).toBeDisabled()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm import' }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('1 Skill versions imported. Sources were left in place.'))

    fireEvent.click(screen.getByRole('button', { name: 'Review global resync' }))
    expect(await screen.findByRole('heading', { name: 'Review managed destinations', level: 2 })).toBeInTheDocument()
    expect(screen.getByText('/home/user/.codex/skills/demo-skill')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm resync' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Global activations resynchronized.')

    const saveCall = calls.find((call) => call.route === '/api/v1/sync/settings' && call.init.method === 'PUT')
    expect(saveCall?.init.body).toBe(JSON.stringify({ syncMode: 'copy-only' }))
    const importCall = calls.find((call) => call.route === '/api/v1/sync/import/preview')
    expect(importCall?.init.body).toBe(JSON.stringify({ selectedGroupIds: ['group-demo'], currentChoices: {}, identityChoices: {} }))
    const resyncCall = calls.find((call) => call.route === '/api/v1/sync/resync/preview')
    expect(resyncCall?.init.body).toBe(JSON.stringify({ scope: 'global' }))
  })

  it('requires an explicit identity choice before importing an aliased source', async () => {
    const source = {
      id: 'alias-source', identifier: 'alias-skill', resolvedIdentifier: 'real-skill', identityConflict: true,
      scope: 'global', location: 'Claude Code', path: '/home/user/.claude/skills/alias-skill',
      realPath: '/home/user/skills/real-skill', checksum: 'sha256-alias', eligible: true,
    }
    const group = {
      id: 'alias-skill@sha256-alias', identifier: 'alias-skill', checksum: 'sha256-alias', versionKey: 'sync-sha256-alias',
      sources: [source], alreadyPresent: false, conflict: false, identityConflict: true,
    }
    const calls: { route: string; body: string }[] = []
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
      const route = String(input)
      const body = String(init.body ?? '')
      calls.push({ route, body })
      if (route === '/api/v1/system') return jsonResponse({})
      if (route === '/api/v1/sync/settings') return jsonResponse({ repositoryPath: '/home/user/.agent-manager/skills', syncMode: 'auto' })
      if (route === '/api/v1/sync/sources') return jsonResponse({ repositoryPath: '/home/user/.agent-manager/skills', locations: [], sources: [source], groups: [group], diagnostics: [] })
      if (route === '/api/v1/sync/import/preview') {
        const { identityChoices = {} } = JSON.parse(body) as { identityChoices?: Record<string, 'entry' | 'resolved'> }
        const resolved = identityChoices['alias-source'] === 'resolved'
        const previewGroup = resolved ? { ...group, id: 'real-skill@sha256-alias', identifier: 'real-skill' } : group
        return jsonResponse({ planId: resolved ? 'resolved-plan' : 'choice-required-plan', preview: {
          repositoryPath: '/home/user/.agent-manager/skills', groups: [previewGroup], selectedGroupIds: [group.id],
          currentChoices: {}, identityChoices, identityChoiceRequired: resolved || identityChoices['alias-source'] === 'entry' ? [] : ['alias-source'],
          repositoryStates: {}, confirmationToken: resolved ? 'resolved-token' : 'choice-token',
        } }, 201)
      }
      if (route === '/api/v1/sync/import/apply') return jsonResponse({ imported: 1, entries: [] })
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: /Storage & Sync/ }))
    await screen.findByRole('heading', { name: 'Storage & Sync', level: 2 })
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Select alias-skill' }))
    fireEvent.click(screen.getByRole('button', { name: 'Review selected import' }))

    expect(await screen.findByText('Choose an identity for each conflicting source and update the preview before importing.')).toBeInTheDocument()
    const confirm = screen.getByRole('button', { name: 'Confirm import' })
    expect(confirm).toBeDisabled()
    fireEvent.click(screen.getByRole('radio', { name: 'Use resolved identity: real-skill' }))
    fireEvent.click(screen.getByRole('button', { name: 'Update preview with this identity' }))
    expect(await screen.findByText('real-skill · New version')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Confirm import' })).toBeEnabled()
    expect(calls.filter((call) => call.route === '/api/v1/sync/import/apply')).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Confirm import' }))
    expect(await screen.findByText('1 Skill versions imported. Sources were left in place.')).toBeInTheDocument()
    const previewBody = JSON.parse(calls.filter((call) => call.route === '/api/v1/sync/import/preview')[1].body)
    expect(previewBody).toEqual({ selectedGroupIds: [group.id], currentChoices: {}, identityChoices: { 'alias-source': 'resolved' } })
    expect(JSON.stringify(previewBody)).not.toContain('/home/user')
  })

  it('previews only selected references and displays their exact versions and impact', async () => {
    const source = {
      id: 'migration-source', identifier: 'demo-skill', scope: 'global', location: 'Claude Code', target: 'claude',
      path: '/home/user/.claude/skills/demo-skill', entryType: 'external-symlink', realPath: '/home/user/.agents/skills/demo-skill',
      originalLink: '../../.agents/skills/demo-skill', linkChain: [{ path: '/home/user/.claude/skills/demo-skill', target: '../../.agents/skills/demo-skill' }],
      checksum: 'sha256-demo', eligible: true, migrationEligible: true,
    }
    const calls: { route: string; body: string }[] = []
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
      const route = String(input)
      const body = String(init.body ?? '')
      calls.push({ route, body })
      if (route === '/api/v1/system') return jsonResponse({})
      if (route === '/api/v1/sync/settings') return jsonResponse({ repositoryPath: '/home/user/.agent-manager/skills', syncMode: 'copy-only' })
      if (route === '/api/v1/sync/sources') return jsonResponse({ repositoryPath: '/home/user/.agent-manager/skills', locations: [], sources: [source], groups: [], diagnostics: [] })
      if (route === '/api/v1/sync/migration/preview') return jsonResponse({ planId: 'migration-plan', preview: {
        scope: 'global', syncMode: 'copy-only', selectedSourceIds: ['migration-source'], entries: [{
          sourceId: 'migration-source', scope: 'global', location: 'Claude Code', target: 'claude', entryPath: source.path,
          originalLink: source.originalLink, linkChain: source.linkChain, realPath: source.realPath, checksum: source.checksum,
          identifier: 'demo-skill', versionKey: 'sha256-demo', versionPath: '/home/user/.agent-manager/skills/demo-skill/versions/sha256-demo',
          method: 'copy-only', impactWarning: 'Other projects that reach this global entry will use the managed version.',
        }], plan: { operation: 'migrate', changes: [{ path: source.path, action: 'copy Skill tree' }], warnings: [] }, warnings: [], confirmationToken: 'migration-token',
      } }, 201)
      if (route === '/api/v1/sync/migration/apply') return jsonResponse({ batchId: 'batch-1', scope: 'global', entries: [{ sourceId: 'migration-source' }] })
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: /Storage & Sync/ }))
    await screen.findByRole('heading', { name: 'Storage & Sync', level: 2 })
    fireEvent.click(await screen.findByRole('checkbox', { name: /Select reference demo-skill/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Review global migration' }))
    expect(await screen.findByRole('heading', { name: 'Review existing references' })).toBeInTheDocument()
    expect(screen.getByText(/\/home\/user\/\.agent-manager\/skills\/demo-skill\/versions\/sha256-demo/)).toBeInTheDocument()
    expect(screen.getAllByText('Link: /home/user/.claude/skills/demo-skill → ../../.agents/skills/demo-skill').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('Other projects that reach this global entry will use the managed version.')).toBeInTheDocument()
    expect(calls.filter((call) => call.route === '/api/v1/sync/migration/apply')).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Confirm reference migration' }))
    expect(await screen.findByText('1 existing references are now managed in the Global scope.')).toBeInTheDocument()
    const previewBody = JSON.parse(calls.find((call) => call.route === '/api/v1/sync/migration/preview')!.body)
    expect(previewBody).toEqual({ selectedSourceIds: ['migration-source'] })
    expect(JSON.stringify(previewBody)).not.toContain('/home/user')
  })

  it('keeps indirect project references selectable after their global target is managed', async () => {
    const source = {
      id: 'indirect-project-reference', identifier: 'demo-skill', scope: 'project', location: 'Codex', target: 'codex',
      path: '/workspace/.codex/skills/demo-skill', entryType: 'external-symlink', realPath: '/home/user/.agent-manager/skills/demo-skill/versions/sha256-demo',
      originalLink: '/home/user/.claude/skills/demo-skill', checksum: 'sha256-demo', eligible: true, migrationEligible: true, alreadyManaged: true,
    }
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const route = String(input)
      if (route === '/api/v1/system') return jsonResponse({ project: { id: 'project-session-id', name: 'sample', path: '/workspace' } })
      if (route === '/api/v1/sync/settings') return jsonResponse({ repositoryPath: '/home/user/.agent-manager/skills', syncMode: 'auto' })
      if (route === '/api/v1/sync/sources') return jsonResponse({ repositoryPath: '/home/user/.agent-manager/skills', locations: [], sources: [source], groups: [], diagnostics: [] })
      if (route === '/api/v1/sync/recovery') return jsonResponse({ batches: [] })
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: /Storage & Sync/ }))
    await screen.findByRole('heading', { name: 'Storage & Sync', level: 2 })

    const reference = await screen.findByRole('checkbox', { name: /Select reference demo-skill/ })
    expect(reference).toBeEnabled()
    fireEvent.click(reference)
    expect(screen.getByRole('button', { name: 'Review project migration' })).toBeEnabled()
  })

  it('shows neutral source entries as import-only', async () => {
    const source = {
      id: 'neutral-source', identifier: 'neutral-skill', scope: 'global', location: '.agents',
      path: '/home/user/.agents/skills/neutral-skill', realPath: '/home/user/.agents/skills/neutral-skill',
      entryType: 'directory', checksum: 'sha256-neutral', eligible: true, migrationEligible: false,
    }
    const group = { id: 'neutral-skill@sha256-neutral', identifier: 'neutral-skill', checksum: 'sha256-neutral', versionKey: 'sync-sha256-neutral', sources: [source], alreadyPresent: false, conflict: false }
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const route = String(input)
      if (route === '/api/v1/system') return jsonResponse({})
      if (route === '/api/v1/sync/settings') return jsonResponse({ repositoryPath: '/home/user/.agent-manager/skills', syncMode: 'auto' })
      if (route === '/api/v1/sync/sources') return jsonResponse({ repositoryPath: '/home/user/.agent-manager/skills', locations: [], sources: [source], groups: [group], diagnostics: [] })
      if (route === '/api/v1/sync/recovery') return jsonResponse({ batches: [] })
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: /Storage & Sync/ }))
    await screen.findByRole('heading', { name: 'Storage & Sync', level: 2 })
    expect(screen.getByText('neutral-skill · Import only')).toBeInTheDocument()
    expect(screen.getByText('This entry can be copied into the library, but its location is not enabled for managed reference migration.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Review global migration' })).toBeDisabled()
  })

  it('requires recovery preview and confirmation before resolving a pending batch', async () => {
    const batch = { batchId: 'batch-recovery', kind: 'resync', scope: 'global', status: 'needs-recovery', actions: ['rollback'], diagnostic: 'one path is published', steps: [
      { id: 'step-1', resource: '/home/user/.codex/skills/demo', action: 'replace', classification: 'after', beforeState: 'old', currentState: 'new' },
    ] }
    const conflictBatch = { batchId: 'conflict-batch', kind: 'migration', scope: 'project', status: 'needs-recovery', actions: [], diagnostic: 'user changed a destination', steps: [] }
    const calls: { route: string; body: string }[] = []
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
      const route = String(input)
      const body = String(init.body ?? '')
      calls.push({ route, body })
      if (route === '/api/v1/system') return jsonResponse({})
      if (route === '/api/v1/sync/settings') return jsonResponse({ repositoryPath: '/home/user/.agent-manager/skills', syncMode: 'auto' })
      if (route === '/api/v1/sync/sources') return jsonResponse({ repositoryPath: '/home/user/.agent-manager/skills', locations: [], sources: [], groups: [], diagnostics: [] })
      if (route === '/api/v1/sync/recovery') return jsonResponse({ batches: calls.some((call) => call.route === '/api/v1/sync/recovery/apply') ? [] : [batch, conflictBatch] })
      if (route === '/api/v1/sync/recovery/preview') return jsonResponse({ planId: 'recovery-plan', preview: { ...batch, steps: [{ ...batch.steps[0], classification: 'after', diagnostic: 'safe to restore' }] } }, 201)
      if (route === '/api/v1/sync/recovery/apply') return jsonResponse({ ...batch, status: 'rolled-back', steps: [] })
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: /Storage & Sync/ }))
    await screen.findByRole('heading', { name: 'Storage & Sync', level: 2 })
    await screen.findByRole('heading', { name: 'Pending recovery', level: 2 })
    expect(calls.filter((call) => call.route === '/api/v1/sync/recovery')).toHaveLength(1)
    expect(await screen.findByText('2 pending')).toBeInTheDocument()
    expect(await screen.findByText(/\/home\/user\/\.codex\/skills\/demo/)).toBeInTheDocument()
    expect(screen.getByText('Conflict detected. Current files and recovery backups are preserved.')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Review rollback' }))
    expect(await screen.findByRole('heading', { name: 'Review pending batch recovery' })).toBeInTheDocument()
    expect(screen.getByText('safe to restore')).toBeInTheDocument()
    expect(calls.filter((call) => call.route === '/api/v1/sync/recovery/apply')).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Confirm rollback' }))
    expect(await screen.findByText('Pending recovery was resolved.')).toBeInTheDocument()
    expect(JSON.parse(calls.find((call) => call.route === '/api/v1/sync/recovery/preview')!.body)).toEqual({ batchId: 'batch-recovery', action: 'rollback' })
    expect(JSON.parse(calls.find((call) => call.route === '/api/v1/sync/recovery/apply')!.body)).toEqual({ planId: 'recovery-plan' })
  })

  it('keeps divergent-version choices visible and requires a refreshed preview before import', async () => {
    const sources = ['version-a', 'version-b'].map((version, index) => ({
      id: `source-${version}`, identifier: 'demo-skill', scope: 'global', location: 'Claude Code',
      path: `/home/user/.claude/skills/${version}`, checksum: `${String.fromCharCode(97 + index).repeat(64)}`, eligible: true,
    }))
    const groups = sources.map((source, index) => ({
      id: `group-${index === 0 ? 'a' : 'b'}`, identifier: source.identifier, checksum: source.checksum,
      versionKey: source.checksum.slice(0, 16), sources: [source], alreadyPresent: false, conflict: false,
      requiresCurrentChoice: true, makeCurrent: false,
    }))
    const calls: { route: string; body: string }[] = []
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
      const route = String(input)
      const body = String(init.body ?? '')
      calls.push({ route, body })
      if (route === '/api/v1/system') return jsonResponse({})
      if (route === '/api/v1/sync/settings') return jsonResponse({ repositoryPath: '/home/user/.agent-manager/skills', syncMode: 'auto' })
      if (route === '/api/v1/sync/sources') return jsonResponse({
        repositoryPath: '/home/user/.agent-manager/skills', locations: [], sources, groups, diagnostics: [],
      })
      if (route === '/api/v1/sync/import/preview') {
        const { currentChoices = {} } = JSON.parse(body) as { currentChoices?: Record<string, string> }
        const chosen = currentChoices['demo-skill']
        return jsonResponse({ planId: `plan-${chosen ?? 'needs-choice'}`, preview: {
          repositoryPath: '/home/user/.agent-manager/skills',
          groups: groups.map((group) => ({ ...group, makeCurrent: group.id === chosen })),
          selectedGroupIds: groups.map((group) => group.id), currentChoices,
          currentChoiceRequired: chosen ? [] : ['demo-skill'], repositoryStates: {}, confirmationToken: `token-${chosen ?? 'needs-choice'}`,
        } }, 201)
      }
      if (route === '/api/v1/sync/import/apply') return jsonResponse({ imported: 2, entries: [] })
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')

    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: /Storage & Sync/ }))
    expect(await screen.findByRole('heading', { name: 'Storage & Sync', level: 2 })).toBeInTheDocument()
    screen.getAllByRole('checkbox', { name: 'Select demo-skill' }).forEach((checkbox) => fireEvent.click(checkbox))
    fireEvent.click(screen.getByRole('button', { name: 'Review selected import' }))
    expect(await screen.findByRole('heading', { name: 'Review before importing', level: 2 })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Confirm import' })).toBeDisabled()

    const choiceRadios = () => Array.from(document.querySelectorAll<HTMLInputElement>('input[name="current-demo-skill"]'))
    fireEvent.click(choiceRadios()[1])
    fireEvent.click(screen.getByRole('button', { name: 'Update preview with this choice' }))
    expect(await screen.findByText('Will become the current version')).toBeInTheDocument()
    await waitFor(() => expect(choiceRadios()[1]).toBeChecked())
    expect(screen.getByRole('button', { name: 'Update preview with this choice' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Confirm import' })).toBeEnabled()

    fireEvent.click(choiceRadios()[0])
    expect(screen.getByRole('button', { name: 'Confirm import' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Update preview with this choice' })).toBeEnabled()
    fireEvent.click(screen.getByRole('button', { name: 'Update preview with this choice' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Confirm import' })).toBeEnabled())
    expect(choiceRadios()[0]).toBeChecked()

    fireEvent.click(screen.getByRole('button', { name: 'Confirm import' }))
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('2 Skill versions imported. Sources were left in place.'))
    const previewCalls = calls.filter((call) => call.route === '/api/v1/sync/import/preview')
    expect(previewCalls.map((call) => JSON.parse(call.body).currentChoices['demo-skill'])).toEqual([undefined, 'group-b', 'group-a'])
  })

  it('shows update check failures beside the button and allows retry', async () => {
    let fail = true
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const route = String(input)
      if (route === '/api/v1/sync/settings') return jsonResponse({ syncMode: 'auto' })
      if (route === '/api/v1/sync/sources') return jsonResponse({ repositoryPath: '/library', locations: [], sources: [], groups: [], diagnostics: [] })
      if (route === '/api/v1/sync/updates/check') {
        if (fail) return jsonResponse({ error: { code: 'sync_timeout', message: 'Update check timed out' } }, 504)
        return jsonResponse({ checkedAt: '2026-10-15T00:00:00Z', items: [] })
      }
      return jsonResponse({})
    }))
    window.history.replaceState(null, '', '/#session=tab-secret')
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: /Storage & Sync/ }))
    const heading = await screen.findByRole('heading', { name: 'Check installed Skill updates' })
    const panel = within(heading.closest('section')!)
    fireEvent.click(panel.getByRole('button', { name: 'Check for updates' }))
    expect(await panel.findByRole('alert')).toHaveTextContent('Update check timed out')
    fail = false
    fireEvent.click(panel.getByRole('button', { name: 'Check for updates' }))
    expect(await panel.findByText('No Skills with recorded sources')).toBeInTheDocument()
    expect(panel.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('checks recorded upstream sources, previews selected or all updates, and waits for confirmation', async () => {
    const updateItems = [
      { id: 'global:remote-a', identifier: 'remote-a', scope: 'global', path: '/home/user/.agents/skills/remote-a', origin: 'acme/skills', originType: 'github', ref: 'main', status: 'available', currentHash: 'old-a', upstreamHash: 'new-a', checkedAt: '2026-10-15T00:00:00Z', canUpdate: true },
      { id: 'project:remote-b', identifier: 'remote-b', scope: 'project', path: '/workspace/.agents/skills/remote-b', origin: 'acme/tools', originType: 'github', ref: 'main', status: 'available', currentHash: 'old-b', upstreamHash: 'new-b', checkedAt: '2026-10-15T00:00:00Z', canUpdate: true },
      { id: 'global:local', identifier: 'local-skill', scope: 'global', path: '/home/user/.agents/skills/local-skill', origin: 'Local', originType: 'local', status: 'local', checkedAt: '2026-10-15T00:00:00Z', canUpdate: false },
    ]
    const calls: { route: string; body: string }[] = []
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
      const route = String(input)
      const body = String(init.body ?? '')
      calls.push({ route, body })
      if (route === '/api/v1/system') return jsonResponse({})
      if (route === '/api/v1/sync/settings') return jsonResponse({ repositoryPath: '/home/user/.agent-manager/skills', syncMode: 'auto' })
      if (route === '/api/v1/sync/sources') return jsonResponse({ repositoryPath: '/home/user/.agent-manager/skills', locations: [], sources: [], groups: [], diagnostics: [], updateUndoAvailable: false })
      if (route === '/api/v1/sync/updates/check') return jsonResponse({ checkedAt: '2026-10-15T00:00:00Z', items: updateItems })
      if (route === '/api/v1/sync/updates/preview') {
        const { selectedSourceIds } = JSON.parse(body) as { selectedSourceIds: string[] }
        return jsonResponse({ planId: 'update-plan', preview: { items: updateItems.filter((item) => selectedSourceIds.includes(item.id)).map((item) => ({ ...item, ref: 'main' })) } }, 201)
      }
      if (route === '/api/v1/sync/updates/apply') return jsonResponse({ updated: 1 })
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')

    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: /Storage & Sync/ }))
    expect(await screen.findByRole('heading', { name: 'Check installed Skill updates', level: 2 })).toBeInTheDocument()
    expect(calls.some((call) => call.route === '/api/v1/sync/updates/check')).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: 'Check for updates' }))
    expect(await screen.findAllByText('Update available')).toHaveLength(2)
    expect(screen.getByText('Local source')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select update for remote-a' }))
    fireEvent.click(screen.getByRole('button', { name: 'Update selected' }))
    expect(await screen.findByRole('heading', { name: 'Review Skill updates', level: 2 })).toBeInTheDocument()
    expect(screen.getAllByText('/home/user/.agents/skills/remote-a')).toHaveLength(2)
    expect(screen.getAllByText('Current hash: old-a')).toHaveLength(2)
    expect(screen.getAllByText('New hash: new-a')).toHaveLength(2)
    expect(calls.some((call) => call.route === '/api/v1/sync/updates/apply')).toBe(false)
    fireEvent.click(screen.getByRole('button', { name: 'Confirm Skill updates' }))
    expect(await screen.findByRole('status')).toHaveTextContent('1 Skills updated from their recorded sources.')

    fireEvent.click(screen.getByRole('button', { name: 'Check for updates' }))
    await screen.findAllByText('Update available')
    fireEvent.click(screen.getByRole('button', { name: 'Update all available' }))
    await screen.findByRole('heading', { name: 'Review Skill updates', level: 2 })
    const previews = calls.filter((call) => call.route === '/api/v1/sync/updates/preview')
    expect(JSON.parse(previews[0].body).selectedSourceIds).toEqual(['global:remote-a'])
    expect(JSON.parse(previews[1].body).selectedSourceIds).toEqual(['global:remote-a', 'project:remote-b'])
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
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

  it('shows recorded Skill source in the management list and details', async () => {
    const skill: Skill = { ...operationSkills[0], source: 'acme/skills', sourceType: 'github', immutableRef: 'abc123' }
    installFixture((route) => {
      if (route === '/api/catalog?q=') return jsonResponse({ skills: [skill], targets: supportedTargets })
      if (route === `/api/v1/skills/${skill.identifier}`) return jsonResponse({ skill })
    })
    renderApp()
    const row = await screen.findByRole('button', { name: /Go Helper/ })
    expect(screen.getByText('Source: acme/skills')).toBeInTheDocument()
    fireEvent.click(row)
    expect(await screen.findByText('acme/skills')).toBeInTheDocument()
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
    const targets = within(actions).getByRole('group', { name: 'Installation targets' })
    fireEvent.click(within(targets).getByRole('checkbox', { name: 'codex' }))
    const preview = within(actions).getByRole('button', { name: 'Preview installation' })
    expect(preview).toBeEnabled()
    preview.focus()
    expect(preview).toHaveFocus()
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
    expect(screen.getByRole('button', { name: '预览安装' })).toBeDisabled()
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

  it('previews installation for one or more selected Skills with explicit targets', async () => {
    const { calls } = installFixture()
    renderApp()
    fireEvent.click(await screen.findByRole('button', { name: /Go Helper/ }))
    expect(screen.queryByRole('button', { name: 'Preview installation' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select go-helper' }))
    expect(screen.getByRole('button', { name: 'Preview installation' })).toBeDisabled()
    const targets = screen.getByRole('group', { name: 'Installation targets' })
    expect(within(targets).getByRole('checkbox', { name: 'codex' })).not.toBeChecked()
    fireEvent.click(within(targets).getByRole('checkbox', { name: 'pi' }))
    await previewBatch()
    expect(JSON.parse(calls.find((call) => call.route === '/api/install/preview')!.init.body as string)).toEqual({ skillIds: ['go-helper'], targets: ['pi'], replaceConflicts: false })
    fireEvent.click(screen.getByRole('button', { name: 'Cancel installation' }))
    expect(screen.getByText('1 selected')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /Go Test/ }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select go-test' }))
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
    expect(screen.getByRole('button', { name: 'Preview installation' })).toBeDisabled()
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
    expect(screen.getByRole('button', { name: 'Preview installation' })).toBeDisabled()
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

  it('preserves conflict confirmation and explicit targets for a single-Skill installation', async () => {
    const { calls } = installFixture()
    renderApp()
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Select go-helper' }))
    const targets = screen.getByRole('group', { name: 'Installation targets' })
    expect(within(targets).getByRole('checkbox', { name: 'codex' })).not.toBeChecked()
    fireEvent.click(within(targets).getByRole('checkbox', { name: 'pi' }))
    fireEvent.click(screen.getByRole('checkbox', { name: /Allow replacement of existing conflicts/ }))
    await previewBatch()
    expect(JSON.parse(calls.find((call) => call.route === '/api/install/preview')!.init.body as string)).toEqual({ skillIds: ['go-helper'], targets: ['pi'], replaceConflicts: true })
    fireEvent.click(screen.getByRole('button', { name: 'Cancel installation' }))
    expect(screen.getByText('1 selected')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select go-test' }))
    expect(within(screen.getByRole('group', { name: 'Installation targets' })).getByRole('checkbox', { name: 'pi' })).toBeChecked()
    expect(screen.getByRole('checkbox', { name: /Allow replacement of existing conflicts/ })).toBeChecked()
  })

  it('preserves Skill selection while reviewing and applying Undo', async () => {
    installFixture((route) => {
      if (route.endsWith('/operations/latest')) return jsonResponse({ latest: { operation: 'activate', resourceKind: 'skill', at: '2026-10-15T00:00:00Z', undoAvailable: true } })
      if (route.endsWith('/plans/undo')) return jsonResponse({ plan: { id: 'undo-plan', operation: 'undo activate', resourceKind: 'skill', changes: [], warnings: [], expiresAt: '2026-10-15T00:10:00Z' } })
      if (route.endsWith('/execute')) return jsonResponse({ operation: 'undo activate', status: 'completed' })
    })
    renderApp()
    fireEvent.click(await screen.findByRole('checkbox', { name: 'Select go-helper' }))
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

  it('bootstraps the token, reads a Skill, previews its installation, and confirms through the local API', async () => {
    const project: Project = { id: 'project-session-id', name: 'sample', path: '/workspace/sample' }
    const skill: Skill = { identifier: 'go-helper', name: 'Go Helper', description: 'Helps with Go projects', tags: ['go'], compatibility: ['codex'], body: 'Use Go tooling.' }
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
      else if (route === '/api/install/preview') payload = { preview: { id: 'opaque-preview-id', skillIds: ['go-helper'], targets: ['codex'], plan: { operation: 'activate', changes: [{ path: '/workspace/sample/.codex/skills/go-helper', action: 'create absolute link', detail: '/library/go-helper' }], warnings: [] } } }
      else if (route === '/api/install/apply') payload = { result: { applied: true, stale: false } }
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
    fireEvent.click(within(screen.getByRole('group', { name: 'Installation targets' })).getByRole('checkbox', { name: 'codex' }))
    fireEvent.click(screen.getByRole('button', { name: 'Preview installation' }))
    expect(await screen.findByRole('heading', { name: 'Review installation', level: 2 })).toBeInTheDocument()
    expect(screen.getByText('/workspace/sample/.codex/skills/go-helper')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Confirm installation' }))
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'Review installation', level: 2 })).not.toBeInTheDocument())
    const previewCall = calls.find((call) => call.route === '/api/install/preview')
    expect(previewCall).toBeDefined()
    expect(new Headers(previewCall?.init.headers).get('Authorization')).toBe('Bearer tab-secret')
    expect(previewCall?.init.body).toBe(JSON.stringify({ skillIds: ['go-helper'], targets: ['codex'], replaceConflicts: false }))
    expect(calls.some((call) => call.route.includes('tab-secret'))).toBe(false)
    expect(JSON.parse(calls.find((call) => call.route === '/api/install/apply')!.init.body as string)).toEqual({ previewId: 'opaque-preview-id' })
    expect(screen.getByText('0 selected')).toBeInTheDocument()
  })

  it('drops a stale removal review after the API rejects execution', async () => {
    const project: Project = { id: 'project-session-id', name: 'sample', path: '/workspace/sample' }
    const plan: OperationPlan = {
      id: 'stale-plan-id', projectId: project.id, operation: 'remove selected skill', version: 'v1', resourceKind: 'skill',
      changes: [{ path: '/workspace/sample/.codex/skills/go-helper', action: 'remove managed link' }],
      warnings: [], createdAt: '2026-09-27T00:00:00Z', expiresAt: '2026-09-27T00:10:00Z', forceReplacementConfirmed: false,
    }
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const route = String(input)
      if (route === '/api/v1/system') return jsonResponse({ project })
      if (route.endsWith('/agents')) return jsonResponse({ agents: [] })
      if (route.endsWith('/recommendations')) return jsonResponse({ project: project.path, scanComplete: true, diagnostics: [], scopes: [] })
      if (route.endsWith('/resources')) return jsonResponse({ resources: [{ target: 'codex', identifier: 'go-helper', path: '/workspace/sample/.codex/skills/go-helper', status: 'managed' }] })
      if (route.endsWith('/plans/remove')) return jsonResponse({ plan })
      if (route.endsWith('/execute')) return jsonResponse({ error: { code: 'stale_plan', message: 'Project state changed. Create a fresh preview.' } }, 409)
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')

    renderApp()
    await screen.findByText('sample')
    fireEvent.click(await screen.findByRole('button', { name: 'Plan removal' }))
    await screen.findByRole('heading', { name: 'remove selected skill' })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm and apply' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Project state changed. Create a fresh preview.')
    expect(screen.queryByRole('heading', { name: 'remove selected skill' })).not.toBeInTheDocument()
  })

  it('closes a terminal removal error and refreshes project state after the journal was committed', async () => {
    const project: Project = { id: 'project-session-id', name: 'sample', path: '/workspace/sample' }
    const plan: OperationPlan = {
      id: 'committed-plan-id', projectId: project.id, operation: 'remove selected skill', version: 'v1', resourceKind: 'skill',
      changes: [{ path: '/workspace/sample/.codex/skills/go-helper', action: 'remove managed link' }],
      warnings: [], createdAt: '2026-09-27T00:00:00Z', expiresAt: '2026-09-27T00:10:00Z', forceReplacementConfirmed: false,
    }
    const calls: string[] = []
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const route = String(input)
      calls.push(route)
      if (route === '/api/v1/system') return jsonResponse({ project })
      if (route.endsWith('/agents')) return jsonResponse({ agents: [] })
      if (route.endsWith('/recommendations')) return jsonResponse({ project: project.path, scanComplete: true, diagnostics: [], scopes: [] })
      if (route.endsWith('/resources')) return jsonResponse({ resources: [{ target: 'codex', identifier: 'go-helper', path: '/workspace/sample/.codex/skills/go-helper', status: 'managed' }] })
      if (route.endsWith('/plans/remove')) return jsonResponse({ plan })
      if (route.endsWith('/execute')) return jsonResponse({ error: { code: 'journal_committed', message: 'The operation was applied and its journal record was published.' } }, 500)
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')

    renderApp()
    await screen.findByText('sample')
    fireEvent.click(await screen.findByRole('button', { name: 'Plan removal' }))
    await screen.findByRole('heading', { name: 'remove selected skill' })
    fireEvent.click(screen.getByRole('button', { name: 'Confirm and apply' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('The operation was applied and its journal record was published.')
    expect(screen.queryByRole('heading', { name: 'remove selected skill' })).not.toBeInTheDocument()
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

  it('separates Doctor findings from project scan notes and reviews a selected repair before applying it', async () => {
    const project: Project = { id: 'project-session-id', name: 'sample', path: '/workspace/sample' }
    const plan: OperationPlan = {
      id: 'doctor-repair-plan', projectId: project.id, operation: 'reconcile', operationRef: { key: 'plan.operation.reconcile' },
      version: 'v1', resourceKind: 'skill',
      changes: [{ path: '/workspace/sample/.codex/skills/stale', action: 'repoint orphaned managed link', actionRef: { key: 'plan.action.repoint_orphaned_link' }, detail: '/library/stale' }],
      warnings: [], createdAt: '2026-10-14T00:00:00Z', expiresAt: '2026-10-14T00:10:00Z', forceReplacementConfirmed: false,
    }
    const calls: { route: string; init: RequestInit }[] = []
    let diagnosticReads = 0
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
      const route = String(input)
      calls.push({ route, init })
      if (route === '/api/v1/system') return jsonResponse({ project })
      if (route.endsWith('/agents')) return jsonResponse({ agents: [] })
      if (route.endsWith('/recommendations')) return jsonResponse({
        project: project.path, scanComplete: false,
        diagnostics: [{ path: '.', message: 'Traversal stopped after inspecting 10000 directory entries.', code: 'scan.traversal_limit', messageRef: { key: 'scan.traversal_limit', args: { limit: '10000' } } }],
        scopes: [],
      })
      if (route.endsWith('/resources')) return jsonResponse({ resources: [] })
      if (route.endsWith('/diagnostics')) {
        diagnosticReads++
        return jsonResponse({ findings: [
          { path: '/workspace/sample/.codex/skills/stale', code: 'doctor.orphaned_managed_link', message: 'orphaned link', repairAction: 'reconcile', messageRef: { key: 'doctor.orphaned_managed_link', args: { target: '/old-library/stale' } } },
          { path: '/workspace/sample/.codex/skills/web-helper', code: 'doctor.git_managed_link_status', message: 'would be tracked', repairAction: 'add_gitignore', messageRef: { key: 'doctor.git_managed_link_status', args: { status: 'would be tracked' } } },
          { path: '/library/invalid-entry', code: 'doctor.invalid_catalog_entry', message: 'invalid catalog entry', messageRef: { key: 'doctor.invalid_catalog_entry', args: { detail: 'missing SKILL.md' } }, guidance: 'Repair the invalid Skill entry in the configured library, then run the scan again.', guidanceRef: { key: 'doctor.guidance.invalid_catalog_entry' } },
        ] })
      }
      if (route.endsWith('/plans/doctor-reconcile')) return jsonResponse({ plan })
      if (route.endsWith('/execute')) return jsonResponse({ operation: 'reconcile', status: 'completed' })
      if (route.endsWith('/operations/latest')) return jsonResponse({ latest: null })
      return jsonResponse({})
    })
    vi.stubGlobal('fetch', fetchMock)
    window.history.replaceState(null, '', '/#session=tab-secret')

    renderApp()
    expect(await screen.findByText('sample')).toBeInTheDocument()
    expect(await screen.findByText('1 project scan note')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /Diagnostics/ }))
    expect(await screen.findByRole('heading', { name: 'Doctor findings', level: 2 })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Project scan notes', level: 2 })).toBeInTheDocument()
    expect(screen.getByText(/Traversal stopped after inspecting 10000/)).toBeInTheDocument()
    expect(screen.getByText('Repair the invalid Skill entry in the configured library, then run the scan again.')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'Simplified Chinese' }))
    expect(await screen.findByText('修复已配置库中的无效 Skill 条目，然后重新扫描。')).toBeInTheDocument()
    expect(screen.getByText('扫描在检查 10000 个目录条目后停止')).toBeInTheDocument()
    fireEvent.click(await screen.findByRole('button', { name: '预览重新关联' }))
    expect(await screen.findByRole('heading', { name: '重新关联', level: 2 })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '确认并应用' }))
    expect(await screen.findByRole('status')).toHaveTextContent('已完成 重新关联，并写入操作日志。')

    const repairCall = calls.find((call) => call.route.endsWith('/plans/doctor-reconcile'))
    expect(repairCall?.init.body).toBe(JSON.stringify({ path: '/workspace/sample/.codex/skills/stale' }))
    await waitFor(() => expect(diagnosticReads).toBeGreaterThan(1))
    expect(calls.some((call) => call.route.endsWith('/resources'))).toBe(true)
    expect(calls.some((call) => call.route.endsWith('/operations/latest'))).toBe(true)
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
    if (route === '/api/install/preview') {
      const payload = JSON.parse(String(init.body ?? '{}')) as { skillIds?: string[]; targets?: string[] }
      return jsonResponse({ preview: {
        id: `preview-${++previews}`, skillIds: payload.skillIds ?? [], targets: payload.targets ?? [],
        plan: { operation: 'activate', changes: [{ path: `/workspace/sample/.${payload.targets?.[0] ?? 'codex'}/skills/${payload.skillIds?.[0] ?? 'go-helper'}`, action: 'create link' }], warnings: [] },
      } })
    }
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

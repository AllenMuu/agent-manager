import { describe, expect, it, vi } from 'vitest'
import { bootstrapSession, sessionStorageKey } from './session'

describe('session bootstrap', () => {
  it('moves a fragment token into tab-scoped storage and removes it from the address', () => {
    const values = new Map<string, string>()
    const storage = {
      getItem: (key: string) => values.get(key) ?? null,
      setItem: (key: string, value: string) => values.set(key, value),
    }
    const replaceAddress = vi.fn()

    const token = bootstrapSession(
      { hash: '#session=short-lived-secret', pathname: '/', search: '?view=workspace' },
      storage,
      replaceAddress,
    )

    expect(token).toBe('short-lived-secret')
    expect(values.get(sessionStorageKey)).toBe('short-lived-secret')
    expect(replaceAddress).toHaveBeenCalledWith('/?view=workspace')
  })

  it('reuses the session only from sessionStorage when the fragment is absent', () => {
    const storage = {
      getItem: (key: string) => key === sessionStorageKey ? 'stored-in-this-tab' : null,
      setItem: vi.fn(),
    }
    expect(bootstrapSession({ hash: '', pathname: '/', search: '' }, storage, vi.fn())).toBe('stored-in-this-tab')
  })
})

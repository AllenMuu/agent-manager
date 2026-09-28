import { afterEach, describe, expect, it, vi } from 'vitest'
import { APIError, apiRequest } from './api'

afterEach(() => vi.unstubAllGlobals())

describe('local API client', () => {
  it('sends the token in Authorization and reports stable API errors', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: { code: 'stale_plan', message: 'Review again.' } }), {
      status: 409,
      headers: { 'Content-Type': 'application/json' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(apiRequest('tab-secret', '/api/v1/plans/opaque/execute', { method: 'POST', body: '{}' }))
      .rejects.toMatchObject<Partial<APIError>>({ code: 'stale_plan', message: 'Review again.', status: 409 })

    expect(fetchMock).toHaveBeenCalledOnce()
    const [route, init] = fetchMock.mock.calls[0] as [string, RequestInit]
    expect(route).toBe('/api/v1/plans/opaque/execute')
    expect(route).not.toContain('tab-secret')
    expect(new Headers(init.headers).get('Authorization')).toBe('Bearer tab-secret')
    expect(init.credentials).toBe('same-origin')
  })
})

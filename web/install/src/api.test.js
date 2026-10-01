import { describe, expect, it } from 'vitest'
import { request } from './api.js'

describe('request', () => {
  it('turns a network failure into an actionable API error', async () => {
    const result = await request('/api/install/apply', {}, 'session-token', async () => {
      throw new TypeError('Failed to fetch')
    })

    expect(result.response.ok).toBe(false)
    expect(result.response.status).toBe(0)
    expect(result.body.error).toMatch(/local Agent Manager service/i)
    expect(result.body.error).toMatch(/retry/i)
  })
})

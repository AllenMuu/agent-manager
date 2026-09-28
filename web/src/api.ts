import type { ErrorEnvelope } from './types'

export class APIError extends Error {
  constructor(
    readonly code: string,
    message: string,
    readonly status: number,
  ) {
    super(message)
    this.name = 'APIError'
  }
}

export async function apiRequest<T>(token: string, route: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  headers.set('Authorization', `Bearer ${token}`)
  if (init.body !== undefined) headers.set('Content-Type', 'application/json')
  const response = await fetch(route, { ...init, headers, credentials: 'same-origin' })
  const payload: unknown = await response.json().catch(() => null)
  if (!response.ok) {
    const error = payload as ErrorEnvelope | null
    throw new APIError(
      error?.error?.code ?? 'request_failed',
      error?.error?.message ?? `Request failed with status ${response.status}.`,
      response.status,
    )
  }
  return payload as T
}

export const get = <T>(token: string, route: string) => apiRequest<T>(token, route)
export const post = <T>(token: string, route: string, body: unknown = {}) =>
  apiRequest<T>(token, route, { method: 'POST', body: JSON.stringify(body) })

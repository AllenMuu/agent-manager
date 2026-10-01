const unreachableMessage = 'Could not reach the local Agent Manager service. Check that it is still running and retry.'

export async function request(path, options = {}, session = '', fetchImpl = globalThis.fetch) {
  try {
    const response = await fetchImpl(path, {
      ...options,
      headers: {
        'X-Agent-Manager-Session': session,
        ...(options.body ? { 'Content-Type': 'application/json' } : {}),
        ...options.headers,
      },
    })
    const body = await response.json().catch(() => ({}))
    return { response, body }
  } catch {
    return {
      response: { ok: false, status: 0 },
      body: { error: unreachableMessage },
    }
  }
}

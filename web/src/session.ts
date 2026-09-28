export const sessionStorageKey = 'agent-manager-session'

export function bootstrapSession(
  locationValue: Pick<Location, 'hash' | 'pathname' | 'search'> = window.location,
  storage: Pick<Storage, 'getItem' | 'setItem'> = window.sessionStorage,
  replaceAddress: (url: string) => void = (url) => window.history.replaceState(null, '', url),
): string | null {
  const fragment = new URLSearchParams(locationValue.hash.replace(/^#/, ''))
  const token = fragment.get('session')
  if (token) {
    storage.setItem(sessionStorageKey, token)
    replaceAddress(`${locationValue.pathname}${locationValue.search}`)
    return token
  }
  return storage.getItem(sessionStorageKey)
}

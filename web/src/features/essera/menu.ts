type SidebarSection = { enabled: boolean; [module: string]: boolean }

/**
 * What a gateway the Console administers never shows, whatever the stored sidebar settings
 * say: drawing and task logs (those routes are closed), the wallet, redemption codes and
 * subscriptions (the Console bills). The server sends the same defaults in `/api/status`.
 */
export const MANAGED_HIDDEN_SIDEBAR_MODULES: Record<string, string[]> = {
  console: ['midjourney', 'task'],
  personal: ['topup'],
  admin: ['redemption', 'subscription'],
}

export function applyManagedSidebarModules<
  T extends Record<string, SidebarSection>,
>(config: T): T {
  const result: Record<string, SidebarSection> = { ...config }
  for (const [section, modules] of Object.entries(
    MANAGED_HIDDEN_SIDEBAR_MODULES
  )) {
    const current = result[section]
    if (!current) continue
    const narrowed: SidebarSection = { ...current }
    for (const module of modules) narrowed[module] = false
    result[section] = narrowed
  }
  return result as T
}

/** The pages of the modules above: closed on a managed gateway even when typed in. */
export const MANAGED_CLOSED_PATHS = [
  '/wallet',
  '/redemption-codes',
  '/subscriptions',
  '/usage-logs/drawing',
  '/usage-logs/task',
]

export function isManagedClosedPath(pathname: string): boolean {
  return MANAGED_CLOSED_PATHS.some(
    (path) => pathname === path || pathname.startsWith(`${path}/`)
  )
}

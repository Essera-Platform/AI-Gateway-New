type SidebarSection = { enabled: boolean; [module: string]: boolean }

/**
 * What a gateway the Console administers never shows, whatever the stored sidebar settings
 * say: the external chat clients (no chat links), drawing and task logs (those routes are
 * closed), the wallet, redemption codes and subscriptions (the Console bills).
 */
export const MANAGED_HIDDEN_SIDEBAR_MODULES: Record<string, string[]> = {
  chat: ['chat'],
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
  '/chat',
  '/chat2link',
  '/wallet',
  '/redemption-codes',
  '/subscriptions',
  '/usage-logs/drawing',
  '/usage-logs/task',
]

/**
 * Public pages a managed gateway does not serve, whose APIs the server closes: the About page
 * and the rankings, and new-api's own registration, password reset and OAuth sign-in (people
 * sign in through Essera).
 */
export const MANAGED_CLOSED_PUBLIC_PATHS = [
  '/about',
  '/rankings',
  '/sign-up',
  '/register',
  '/forgot-password',
  '/reset',
  '/user/reset',
  '/oauth',
]

function matchesAny(paths: string[], pathname: string): boolean {
  return paths.some(
    (path) => pathname === path || pathname.startsWith(`${path}/`)
  )
}

export function isManagedClosedPath(pathname: string): boolean {
  return matchesAny(MANAGED_CLOSED_PATHS, pathname)
}

export function isManagedClosedPublicPath(pathname: string): boolean {
  return matchesAny(MANAGED_CLOSED_PUBLIC_PATHS, pathname)
}

/** The Essera mark of `essera-logo.tsx` as a file, served from `public/`. */
export const ESSERA_FAVICON = '/essera-favicon.svg'

/**
 * The logo address a status names: the operator's own (`logo`, from ESSERA_LOGO on a managed
 * gateway), else the Essera mark on a managed gateway, else none.
 */
export function esseraFavicon(status: Record<string, unknown>): string {
  const logo = typeof status.logo === 'string' ? status.logo : ''
  if (logo) return logo
  return status.essera_managed === true ? ESSERA_FAVICON : ''
}

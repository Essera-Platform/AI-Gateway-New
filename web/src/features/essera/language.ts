import i18n from 'i18next'

/** A managed gateway's only interface language. */
export const ESSERA_LANGUAGE = 'en'

/** Whether the last status this browser saw was a managed gateway's (before the first fetch). */
export function cachedStatusIsManaged(): boolean {
  try {
    const raw = window.localStorage.getItem('status')
    return raw ? JSON.parse(raw)?.essera_managed === true : false
  } catch {
    return false
  }
}

/**
 * Puts a managed gateway's interface in English, whatever the browser or a saved preference
 * says: the gateway is English only.
 */
export function enforceEsseraLanguage(status: Record<string, unknown>): void {
  if (status.essera_managed !== true) return
  if (i18n.language === ESSERA_LANGUAGE) return
  void i18n.changeLanguage(ESSERA_LANGUAGE)
}

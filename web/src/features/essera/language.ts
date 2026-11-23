import i18n from 'i18next'

import { convertDetectedLanguage } from '@/i18n/languages'

/** A managed gateway's default interface language. */
export const ESSERA_LANGUAGE = 'en'

/** The interface languages a managed gateway offers, in the switcher's order. */
export const ESSERA_LANGUAGE_OPTIONS = [
  { code: 'en', label: 'English' },
  { code: 'zhCN', label: '中文' },
] as const

export const ESSERA_LANGUAGES: string[] = ESSERA_LANGUAGE_OPTIONS.map(
  (option) => option.code
)

/** Where i18next keeps the chosen language (its detector's default key). */
const LANGUAGE_STORAGE_KEY = 'i18nextLng'

/** Whether the last status this browser saw was a managed gateway's (before the first fetch). */
export function cachedStatusIsManaged(): boolean {
  try {
    const raw = window.localStorage.getItem('status')
    return raw ? JSON.parse(raw)?.essera_managed === true : false
  } catch {
    return false
  }
}

function savedLanguage(): string | null {
  try {
    const saved = window.localStorage.getItem(LANGUAGE_STORAGE_KEY)
    return saved ? convertDetectedLanguage(saved) : null
  } catch {
    return null
  }
}

/**
 * The language this browser chose before i18next started. Read at import, ahead of the
 * detector, which caches whatever it guessed from the browser under the same key.
 */
const choiceAtStart = typeof window === 'undefined' ? null : savedLanguage()
let firstStatusSeen = false

/**
 * Keeps a managed gateway's interface in English or Chinese. A saved choice of one of them
 * stays; anything else, including a guess from the browser on the first visit, reads English.
 */
export function enforceEsseraLanguage(status: Record<string, unknown>): void {
  if (status.essera_managed !== true) return
  const wanted = firstStatusSeen ? i18n.language : choiceAtStart
  firstStatusSeen = true
  const language =
    wanted && ESSERA_LANGUAGES.includes(wanted) ? wanted : ESSERA_LANGUAGE
  if (i18n.language === language) return
  void i18n.changeLanguage(language)
}

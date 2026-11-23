import { normalizeInterfaceLanguage } from '@/i18n/languages'

export type DocumentLanguage = 'en' | 'zh'

/** The document is in Simplified Chinese or English; other interface languages read English. */
export function documentLanguage(interfaceLanguage?: string): DocumentLanguage {
  return normalizeInterfaceLanguage(interfaceLanguage) === 'zhCN' ? 'zh' : 'en'
}

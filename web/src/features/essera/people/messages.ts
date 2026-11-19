import type { TFunction } from 'i18next'

import { translateServerText } from '@/lib/server-error-message'

import type { EsseraPeopleResponse } from './api'

/** The Gateway's answer to a refused people request, in the reader's language. */
export function peopleMessage(
  t: TFunction,
  response: EsseraPeopleResponse<unknown>
): string {
  const text = response.message_key || response.message
  if (!text) return t('Data is temporarily unavailable')
  return translateServerText(t, text)
}

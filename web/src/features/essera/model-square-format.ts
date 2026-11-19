import type { TFunction } from 'i18next'

import { formatSpend } from './account'

/**
 * One of the person's prices as the Console rates it: `amount` for `per_quantity` units
 * (1000000 tokens, 1 image, 1 second, 1 minute, 1 request). Shown as given, never rescaled.
 */
export type EsseraPrice = {
  unit: string
  amount: string
  currency: string
  per_quantity?: number
  provider?: string
  model?: string
}

/** What a managed gateway's Model Square says about one model. */
export type EsseraModelInfo = {
  signedIn: boolean
  pricesAvailable: boolean
  displayName: string
  modelType: string
  prices: EsseraPrice[]
}

const UNIT_LABEL_KEYS: Record<
  string,
  { label: string; one: string; many: string }
> = {
  input_tokens: { label: 'Input', one: 'token', many: '{{count}} tokens' },
  output_tokens: { label: 'Output', one: 'token', many: '{{count}} tokens' },
  images: { label: 'Image', one: 'image', many: '{{count}} images' },
  video_seconds: { label: 'Video', one: 'second', many: '{{count}} seconds' },
  audio_minutes: { label: 'Audio', one: 'minute', many: '{{count}} minutes' },
  requests: { label: 'Request', one: 'request', many: '{{count}} requests' },
}

const MODEL_TYPE_LABEL_KEYS: Record<string, string> = {
  text: 'Text',
  image: 'Image',
  video: 'Video',
  audio: 'Audio',
  embedding: 'Embedding',
}

export function esseraModelTypeLabel(t: TFunction, modelType: string): string {
  const key = MODEL_TYPE_LABEL_KEYS[modelType]
  return key ? t(key) : modelType
}

function perLabel(t: TFunction, unit: string, perQuantity: number): string {
  const keys = UNIT_LABEL_KEYS[unit]
  const perMillionTokens = perQuantity === 1_000_000 && unit.endsWith('_tokens')
  if (perMillionTokens) return t('1M tokens')
  if (perQuantity === 1) return keys ? t(keys.one) : unit
  if (keys) return t(keys.many, { count: perQuantity })
  return `${perQuantity} ${unit}`
}

/** "Input", "3.00 USD", "1M tokens": the parts of one price as people read them. */
export function esseraPriceParts(
  t: TFunction,
  price: EsseraPrice
): { label: string; value: string; per: string } {
  const keys = UNIT_LABEL_KEYS[price.unit]
  return {
    label: keys ? t(keys.label) : price.unit,
    value: formatSpend(price.amount, price.currency),
    per: perLabel(t, price.unit, price.per_quantity ?? 1),
  }
}

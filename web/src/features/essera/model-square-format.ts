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

/**
 * A model's availability and speed over the last 24 hours, as the Console measured them from
 * every customer's requests and its own probes. A figure with no data is null.
 */
export type EsseraModelMetrics = {
  provider: string
  model: string
  availability_24h: number | null
  p50_ttft_ms: number | null
  p95_ttft_ms: number | null
  p50_latency_ms: number | null
  output_per_second: number | null
  traffic_requests_24h: number
  probe_requests_24h: number
  checked_at: string | null
}

/** What a managed gateway's Model Square says about one model. */
export type EsseraModelInfo = {
  signedIn: boolean
  pricesAvailable: boolean
  displayName: string
  modelType: string
  prices: EsseraPrice[]
  metrics?: EsseraModelMetrics | undefined
}

const TOKENS = { one: 'token', many: '{{count}} tokens' }

/**
 * The Console's units, in the order a bill lists them. The `_long` units are the prices of a
 * request whose prompt is over the model's long-context size; they then apply to all its tokens.
 */
const UNIT_LABEL_KEYS: Record<
  string,
  { label: string; one: string; many: string }
> = {
  input_tokens: { label: 'Input', ...TOKENS },
  cache_read_tokens: { label: 'Cache read', ...TOKENS },
  cache_write_tokens: { label: 'Cache write (5 min)', ...TOKENS },
  cache_write_1h_tokens: { label: 'Cache write (1 h)', ...TOKENS },
  output_tokens: { label: 'Output', ...TOKENS },
  input_tokens_long: { label: 'Long context input', ...TOKENS },
  cache_read_tokens_long: { label: 'Long context cache read', ...TOKENS },
  cache_write_tokens_long: {
    label: 'Long context cache write (5 min)',
    ...TOKENS,
  },
  cache_write_1h_tokens_long: {
    label: 'Long context cache write (1 h)',
    ...TOKENS,
  },
  output_tokens_long: { label: 'Long context output', ...TOKENS },
  input_images: {
    label: 'Input image',
    one: 'image',
    many: '{{count}} images',
  },
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

const UNIT_ORDER = Object.keys(UNIT_LABEL_KEYS)

/** Prices in the order a bill lists their units; units this gateway does not know go last. */
export function inBillOrder<T extends { unit: string }>(
  items: readonly T[]
): T[] {
  const rank = (unit: string) => {
    const index = UNIT_ORDER.indexOf(unit)
    return index === -1 ? UNIT_ORDER.length : index
  }
  return [...items].sort((a, b) => rank(a.unit) - rank(b.unit))
}

/** "Cache read", "Input image": a unit as people read it. */
export function esseraUnitLabel(t: TFunction, unit: string): string {
  const keys = UNIT_LABEL_KEYS[unit]
  return keys ? t(keys.label) : unit
}

export function isLongContextUnit(unit: string): boolean {
  return unit.endsWith('_long')
}

function perLabel(t: TFunction, unit: string, perQuantity: number): string {
  const keys = UNIT_LABEL_KEYS[unit]
  const perMillionTokens = perQuantity === 1_000_000 && unit.includes('_tokens')
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
  return {
    label: esseraUnitLabel(t, price.unit),
    value: formatSpend(price.amount, price.currency),
    per: perLabel(t, price.unit, price.per_quantity ?? 1),
  }
}

const MINUTE_MS = 60_000

/** "Checked just now", "Checked 5 min ago", "Checked 3 h ago". */
export function checkedAgo(
  t: TFunction,
  checkedAt: string | null,
  now: number = Date.now()
): string | null {
  if (!checkedAt) return null
  const at = Date.parse(checkedAt)
  if (Number.isNaN(at)) return null
  const minutes = Math.max(0, Math.floor((now - at) / MINUTE_MS))
  if (minutes < 1) return t('Checked just now')
  if (minutes < 60) return t('Checked {{minutes}} min ago', { minutes })
  return t('Checked {{hours}} h ago', { hours: Math.floor(minutes / 60) })
}

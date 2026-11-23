import type { TFunction } from 'i18next'

import { formatNumber } from '@/lib/format'

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
  temporarilyUnavailable?: boolean
}

const TOKENS = { one: 'token', many: '{{count}} tokens' }
const SECONDS = { one: 'second', many: '{{count}} seconds' }

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
  speech_input_tokens: { label: 'Speech input', ...TOKENS },
  speech_output_tokens: { label: 'Speech output', ...TOKENS },
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
  input_texts: { label: 'Text input', one: 'text', many: '{{count}} texts' },
  input_video_seconds: { label: 'Video input', ...SECONDS },
  input_audio_seconds: { label: 'Audio input', ...SECONDS },
  images: { label: 'Image', one: 'image', many: '{{count}} images' },
  video_seconds: { label: 'Video', ...SECONDS },
  audio_minutes: { label: 'Audio', one: 'minute', many: '{{count}} minutes' },
  search_units: {
    label: 'Search unit',
    one: 'search unit',
    many: '{{count}} search units',
  },
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

/** Every unit the Console prices and measures, in the order a bill lists them. */
export const ESSERA_UNITS: readonly string[] = Object.keys(UNIT_LABEL_KEYS)
const UNIT_ORDER = ESSERA_UNITS

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

/** "Cache read", "Input image": a unit as people read it; a unit this gateway does not know
 * yet reads as its words rather than its key. */
export function esseraUnitLabel(t: TFunction, unit: string): string {
  const keys = UNIT_LABEL_KEYS[unit]
  return keys ? t(keys.label) : unit.replaceAll('_', ' ')
}

/** The tokens new-api's own figures show (input and output, cache reads and writes); every
 * other unit is shown beside them, by name. */
export const TOKEN_FIGURE_UNITS: ReadonlySet<string> = new Set([
  'input_tokens',
  'output_tokens',
  'cache_read_tokens',
  'cache_write_tokens',
  'cache_write_1h_tokens',
])

/** Tokens of any kind: text, cached, speech, at the long-context price. */
export function isTokenUnit(unit: string): boolean {
  return unit.endsWith('_tokens') || unit.includes('_tokens_')
}

/** "1,200 tokens", "1 image", "3 search units": a quantity of a unit as people read it. */
export function esseraUnitQuantity(
  t: TFunction,
  unit: string,
  quantity: number,
  locale?: Intl.LocalesArgument
): string {
  const keys = UNIT_LABEL_KEYS[unit]
  const count = formatNumber(quantity, locale)
  if (!keys) return `${count} ${unit.replaceAll('_', ' ')}`
  if (quantity === 1) return `${count} ${t(keys.one)}`
  return t(keys.many, { count })
}

/** "Search unit: 3", "Video input: 1,200": a unit and its quantity in a list of units. */
export function esseraUnitFigure(
  t: TFunction,
  unit: string,
  quantity: number,
  locale?: Intl.LocalesArgument
): string {
  return `${esseraUnitLabel(t, unit)}: ${formatNumber(quantity, locale)}`
}

/** The units a request or a window measured, by the name the Console's prices give them. */
export type EsseraUnits = Record<string, number>

/** The units with a quantity, in the order a bill lists them. */
export function measuredUnits(
  units: EsseraUnits | null | undefined
): { unit: string; quantity: number }[] {
  return inBillOrder(
    Object.entries(units ?? {})
      .filter(([, quantity]) => Number.isFinite(quantity) && quantity > 0)
      .map(([unit, quantity]) => ({ unit, quantity }))
  )
}

/** Adds up the units of several requests or windows. */
export function addUnits(
  into: EsseraUnits,
  units: EsseraUnits | null | undefined
): EsseraUnits {
  for (const [unit, quantity] of Object.entries(units ?? {})) {
    if (!Number.isFinite(quantity) || quantity <= 0) continue
    into[unit] = (into[unit] ?? 0) + quantity
  }
  return into
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

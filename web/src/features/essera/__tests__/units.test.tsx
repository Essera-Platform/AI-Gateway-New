import { cleanup, render, screen } from '@testing-library/react'
import { createInstance, type TFunction } from 'i18next'
import { afterEach, describe, expect, it } from 'vitest'

import { EsseraMeasuredUnits, EsseraOtherUnits } from '../measured-units'
import {
  ESSERA_UNITS,
  esseraPriceParts,
  esseraUnitLabel,
  esseraUnitQuantity,
  measuredUnits,
} from '../model-square-format'

afterEach(cleanup)

const t = (await (async () => {
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: {} })
  return i18n.t
})()) as TFunction

// The units the Console prices and measures (EnteredUnit in its rates/models.py).
const CONSOLE_UNITS = [
  'input_tokens',
  'output_tokens',
  'cache_read_tokens',
  'cache_write_tokens',
  'cache_write_1h_tokens',
  'input_tokens_long',
  'output_tokens_long',
  'cache_read_tokens_long',
  'cache_write_tokens_long',
  'cache_write_1h_tokens_long',
  'input_images',
  'input_texts',
  'input_video_seconds',
  'input_audio_seconds',
  'requests',
  'images',
  'video_seconds',
  'audio_minutes',
  'search_units',
  'speech_input_tokens',
  'speech_output_tokens',
]

describe('the Console’s units as people read them', () => {
  it.each(CONSOLE_UNITS)('gives %s a label and never its key', (unit) => {
    expect(ESSERA_UNITS).toContain(unit)
    expect(esseraUnitLabel(t, unit)).not.toContain('_')
    expect(esseraUnitQuantity(t, unit, 2)).not.toContain('_')
  })

  it('names the units of rerank and Marengo models', () => {
    expect(esseraUnitLabel(t, 'search_units')).toBe('Search unit')
    expect(esseraUnitLabel(t, 'input_texts')).toBe('Text input')
    expect(esseraUnitLabel(t, 'input_video_seconds')).toBe('Video input')
    expect(esseraUnitLabel(t, 'input_audio_seconds')).toBe('Audio input')
    expect(esseraUnitQuantity(t, 'search_units', 3)).toBe('3 search units')
    expect(esseraUnitQuantity(t, 'input_texts', 1)).toBe('1 text')
    expect(esseraUnitQuantity(t, 'input_video_seconds', 1200, 'en')).toBe(
      '1,200 seconds'
    )
  })

  it('prices a rerank model per search unit, not by its key', () => {
    expect(
      esseraPriceParts(t, {
        unit: 'search_units',
        amount: '0.002000',
        currency: 'USD',
        per_quantity: 1,
      })
    ).toMatchObject({ label: 'Search unit', per: 'search unit' })
  })

  it('reads a unit it does not know yet as words', () => {
    expect(esseraUnitLabel(t, 'output_video_seconds')).toBe(
      'output video seconds'
    )
  })

  it('lists only units with a quantity, in bill order', () => {
    expect(
      measuredUnits({
        search_units: 3,
        output_tokens: 0,
        input_tokens: 12,
        cache_read_tokens: 4,
      })
    ).toEqual([
      { unit: 'input_tokens', quantity: 12 },
      { unit: 'cache_read_tokens', quantity: 4 },
      { unit: 'search_units', quantity: 3 },
    ])
    expect(measuredUnits(undefined)).toEqual([])
  })
})

describe('the units of a request in its log', () => {
  it('shows what it measured besides the input, output and cache tokens', () => {
    render(
      <EsseraOtherUnits
        units={{ input_tokens: 10, images: 2, speech_output_tokens: 5 }}
      />
    )

    expect(screen.getByText('Speech output: 5 · Image: 2')).toBeVisible()
  })

  it('shows nothing when a request measured only those tokens', () => {
    const { container } = render(
      <EsseraOtherUnits units={{ input_tokens: 10, cache_read_tokens: 3 }} />
    )

    expect(container).toBeEmptyDOMElement()
  })

  it('lists every unit in the details with its label and quantity', () => {
    render(
      <EsseraMeasuredUnits
        units={{ search_units: 3, input_tokens: 1200, cache_read_tokens: 0 }}
      />
    )

    expect(screen.getByText('Input').parentElement).toHaveTextContent('1,200')
    expect(screen.getByText('Search unit').parentElement).toHaveTextContent('3')
    expect(screen.queryByText('Cache read')).not.toBeInTheDocument()
  })
})

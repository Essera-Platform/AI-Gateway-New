import { describe, expect, test } from 'vitest'

import { DEFAULT_PARAMETER_ENABLED } from '../../../constants'
import {
  effectiveParameterEnabled,
  modelParameterSupport,
} from '../model-parameters'

const priced = [
  {
    model_name: 'anthropic.claude-opus-5-5',
    supported_parameters: ['max_tokens', 'stop', 'stream'],
  },
  {
    model_name: 'anthropic.claude-sonnet-4-6',
    supported_parameters: ['temperature', 'top_p', 'max_tokens', 'stop'],
    sampling_exclusive: true,
  },
  {
    model_name: 'amazon.nova-lite-v1:0',
    supported_parameters: ['temperature', 'top_p', 'max_tokens', 'stop'],
  },
]

const allOn = {
  temperature: true,
  top_p: true,
  max_tokens: true,
  frequency_penalty: true,
  presence_penalty: true,
  seed: true,
}

function sentFor(model: string) {
  return effectiveParameterEnabled(
    allOn,
    modelParameterSupport(priced, model, true)
  )
}

describe('effectiveParameterEnabled', () => {
  test('a Claude model that rejects sampling gets neither temperature nor top_p', () => {
    expect(sentFor('anthropic.claude-opus-5-5')).toEqual({
      ...allOn,
      temperature: false,
      top_p: false,
      frequency_penalty: false,
      presence_penalty: false,
      seed: false,
    })
  })

  test('a model taking one sampling control keeps temperature and drops top_p', () => {
    const sent = sentFor('anthropic.claude-sonnet-4-6')
    expect(sent.temperature).toBe(true)
    expect(sent.top_p).toBe(false)
  })

  test('switching to a model that takes both sends both again', () => {
    const sent = sentFor('amazon.nova-lite-v1:0')
    expect([sent.temperature, sent.top_p, sent.max_tokens]).toEqual([
      true,
      true,
      true,
    ])
  })

  test('penalties and seed are never sent, even by default', () => {
    const sent = effectiveParameterEnabled(
      DEFAULT_PARAMETER_ENABLED,
      modelParameterSupport(priced, 'amazon.nova-lite-v1:0', true)
    )
    expect([sent.frequency_penalty, sent.presence_penalty, sent.seed]).toEqual(
      [false, false, false]
    )
  })

  test('a model the pricing list does not name gets max_tokens only', () => {
    const sent = sentFor('vendor.unknown')
    expect(sent).toEqual({
      ...allOn,
      temperature: false,
      top_p: false,
      frequency_penalty: false,
      presence_penalty: false,
      seed: false,
    })
  })
})

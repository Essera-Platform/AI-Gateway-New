import { describe, expect, test } from 'vitest'

import type { PricingModel } from '../../types'
import { buildSupportedParameters } from '../mock-stats'

function names(model: Partial<PricingModel>) {
  return buildSupportedParameters(model as PricingModel).map((p) => p.name)
}

describe('buildSupportedParameters', () => {
  test('a managed model lists exactly the parameters the gateway measured', () => {
    expect(
      names({
        model_name: 'anthropic.claude-opus-5-5',
        model_type: 'text',
        supported_parameters: ['max_tokens', 'stop', 'stream'],
      })
    ).toEqual(['max_tokens', 'stop', 'stream'])
  })

  test('an embedding model lists its own parameters', () => {
    expect(
      names({
        model_name: 'cohere.embed-english-v3',
        model_type: 'embedding',
        supported_parameters: ['input', 'encoding_format'],
      })
    ).toEqual(['input', 'encoding_format'])
  })
})

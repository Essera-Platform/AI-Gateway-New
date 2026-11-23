import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'

import { DEFAULT_CONFIG } from '../../../constants'
import type { ModelParameterSupport } from '../../../lib/parameters/model-parameters'
import { PlaygroundParameterPanel } from '../playground-parameter-panel'

const enabled = {
  temperature: true,
  top_p: false,
  max_tokens: true,
  frequency_penalty: false,
  presence_penalty: false,
  seed: false,
}

async function openPanel(support: ModelParameterSupport) {
  render(
    <PlaygroundParameterPanel
      config={DEFAULT_CONFIG}
      onConfigChange={vi.fn()}
      onParameterEnabledChange={vi.fn()}
      parameterEnabled={enabled}
      parameterSupport={support}
    />
  )
  await userEvent.click(screen.getByRole('button', { name: 'Parameters' }))
}

describe('PlaygroundParameterPanel', () => {
  test('a model rejecting sampling shows only Max Tokens', async () => {
    await openPanel({
      supported: ['max_tokens', 'stop', 'stream'],
      samplingExclusive: false,
    })
    expect(screen.getByRole('switch', { name: /Max Tokens/ })).toBeTruthy()
    expect(screen.queryByRole('switch', { name: /Temperature/ })).toBeNull()
    expect(screen.queryByRole('switch', { name: /Top P/ })).toBeNull()
    expect(screen.queryByRole('switch', { name: /Penalty/ })).toBeNull()
  })

  test('a model taking one sampling control explains the choice', async () => {
    await openPanel({
      supported: ['temperature', 'top_p', 'max_tokens'],
      samplingExclusive: true,
    })
    expect(screen.getByRole('switch', { name: /Temperature/ })).toBeTruthy()
    expect(screen.getByRole('switch', { name: /Top P/ })).toBeTruthy()
    expect(
      screen.getByText('This model takes Temperature or Top P, not both.')
    ).toBeTruthy()
  })
})

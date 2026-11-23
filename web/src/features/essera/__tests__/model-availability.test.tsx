import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it } from 'vitest'

import { ModelGroupSelector } from '@/components/model-group-selector'
import { withAvailabilityNotes } from '@/features/essera/model-availability'

const fable = {
  label: 'anthropic.claude-fable-5',
  value: 'anthropic.claude-fable-5',
}
const nova = { label: 'amazon.nova-lite-v1:0', value: 'amazon.nova-lite-v1:0' }
const note = 'Temporarily unavailable at AWS'

afterEach(() => cleanup())

describe('Playground model picker availability notes', () => {
  it('notes only the models the Gateway marked unavailable', () => {
    const options = withAvailabilityNotes(
      [fable, nova],
      [
        { model_name: fable.value, temporarily_unavailable: true },
        { model_name: nova.value },
      ],
      note
    )

    expect(options).toEqual([{ ...fable, note }, nova])
  })

  it('keeps the options as they are when no model is marked', () => {
    const options = [fable, nova]

    expect(withAvailabilityNotes(options, [], note)).toBe(options)
  })

  it('shows the note under the model name and keeps the model selectable', async () => {
    render(
      <ModelGroupSelector
        selectedModel={nova.value}
        models={withAvailabilityNotes(
          [fable, nova],
          [{ model_name: fable.value, temporarily_unavailable: true }],
          note
        )}
        onModelChange={() => undefined}
        selectedGroup='essera'
        groups={[{ label: 'essera', value: 'essera' }]}
        onGroupChange={() => undefined}
        hideGroups
      />
    )

    await userEvent.click(screen.getByRole('combobox'))

    const unavailable = await screen.findByRole('option', {
      name: new RegExp(fable.label),
    })
    expect(unavailable).toHaveTextContent(note)
    expect(unavailable).not.toHaveAttribute('aria-disabled', 'true')
    expect(
      screen.getByRole('option', { name: nova.label })
    ).not.toHaveTextContent(note)
  })
})

import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it } from 'vitest'

import { ModelGroupSelector } from '../../model-group-selector'

const models = [{ label: 'Nova Lite', value: 'eu.amazon.nova-lite-v1:0' }]
const groups = [{ label: 'essera', value: 'essera' }]

function renderSelector(hideGroups?: boolean) {
  render(
    <ModelGroupSelector
      selectedModel='eu.amazon.nova-lite-v1:0'
      models={models}
      onModelChange={() => undefined}
      selectedGroup='essera'
      groups={groups}
      onGroupChange={() => undefined}
      hideGroups={hideGroups}
    />
  )
}

afterEach(() => cleanup())

describe('model and group selector', () => {
  it('offers only models when groups are hidden', async () => {
    renderSelector(true)
    const trigger = screen.getByRole('combobox')

    expect(trigger).not.toHaveTextContent('essera')
    await userEvent.click(trigger)
    expect(
      await screen.findByRole('option', { name: 'Nova Lite' })
    ).toBeInTheDocument()
    expect(screen.queryByText('Model Group')).not.toBeInTheDocument()
  })

  it('keeps the group column by default', async () => {
    renderSelector()
    const trigger = screen.getByRole('combobox')

    expect(trigger).toHaveTextContent('essera')
    await userEvent.click(trigger)
    expect(await screen.findByText('Model Group')).toBeInTheDocument()
  })
})

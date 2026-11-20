import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { EsseraCharges } from '../charges'

afterEach(cleanup)

describe('a request’s charges from the Console', () => {
  it('lists each unit with its quantity and amount, cache lines included, then the total', () => {
    render(
      <EsseraCharges
        charges={[
          {
            unit: 'output_tokens',
            quantity: 340,
            amount: '0.003740',
            currency: 'USD',
          },
          {
            unit: 'cache_write_1h_tokens',
            quantity: 200,
            amount: '0.000880',
            currency: 'USD',
          },
          {
            unit: 'input_tokens',
            quantity: 1200,
            amount: '0.002640',
            currency: 'USD',
          },
          {
            unit: 'cache_read_tokens',
            quantity: 5000,
            amount: '0.001100',
            currency: 'USD',
          },
          {
            unit: 'cache_write_tokens',
            quantity: 800,
            amount: '0.002200',
            currency: 'USD',
          },
        ]}
        total='0.010560'
        currency='USD'
      />
    )

    const rows = [
      ['Input', '1,200 · 0.002640 USD'],
      ['Cache read', '5,000 · 0.001100 USD'],
      ['Cache write (5 min)', '800 · 0.002200 USD'],
      ['Cache write (1 h)', '200 · 0.000880 USD'],
      ['Output', '340 · 0.003740 USD'],
      ['Total Cost', '0.010560 USD'],
    ]
    const labels = rows.map(([label]) => screen.getByText(label))
    rows.forEach(([, value], i) => {
      expect(labels[i].parentElement).toHaveTextContent(value)
      if (i > 0) {
        expect(
          labels[i - 1].compareDocumentPosition(labels[i]) &
            Node.DOCUMENT_POSITION_FOLLOWING
        ).toBeTruthy()
      }
    })
  })

  it('says a unit the Console has not priced is not priced yet, never a zero', () => {
    render(
      <EsseraCharges
        charges={[
          { unit: 'input_images', quantity: 2, amount: null, currency: null },
        ]}
        total={null}
        currency='USD'
      />
    )

    expect(screen.getByText('Input image').parentElement).toHaveTextContent(
      '2 · Not priced yet'
    )
    expect(screen.getByText('Total Cost').parentElement).toHaveTextContent(
      '- USD'
    )
  })
})

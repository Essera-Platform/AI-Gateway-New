import { describe, expect, it } from 'vitest'

import { splitFigure } from '../brand/figure'

describe('splitFigure', () => {
  it.each([
    ['100.00 USD', '100.00', ' USD'],
    ['99.9%', '99.9', '%'],
    ['18ms', '18', 'ms'],
    ['$0.1351', '$0.1351', ''],
    ['208.2K', '208.2K', ''],
    ['42', '42', ''],
  ])('splits %s into the figure and its unit', (text, figure, unit) => {
    expect(splitFigure(text)).toEqual({ figure, unit })
  })

  it('keeps text without a digit whole', () => {
    expect(splitFigure('--')).toEqual({ figure: '--', unit: '' })
  })
})

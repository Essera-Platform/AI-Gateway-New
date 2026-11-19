import { describe, expect, it } from 'vitest'

import { isManagedClosedPath } from '../menu'

describe('Pages the Console owns on a managed gateway', () => {
  it.each([
    ['/wallet', true],
    ['/wallet/history', true],
    ['/redemption-codes', true],
    ['/subscriptions', true],
    ['/usage-logs/task', true],
    ['/usage-logs/drawing', true],
    ['/usage-logs/common', false],
    ['/usage-logs/audit', false],
    ['/walletx', false],
    ['/keys', false],
  ])('%s closed: %s', (path, closed) => {
    expect(isManagedClosedPath(path)).toBe(closed)
  })
})

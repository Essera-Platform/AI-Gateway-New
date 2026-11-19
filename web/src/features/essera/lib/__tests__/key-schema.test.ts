import type { TFunction } from 'i18next'
import { describe, expect, it } from 'vitest'

import { daysFromNow, getKeySchema, toKeyPayload } from '../key-schema'

const t = ((key: string) => key) as TFunction
const now = Date.UTC(2026, 10, 19, 12)
const schema = getKeySchema(t, () => now)

describe('key expiry', () => {
  it('accepts 90 days and sends unix seconds', () => {
    const values = schema.parse({
      name: ' Production ',
      expiresAt: daysFromNow(90, now),
    })

    expect(toKeyPayload(values)).toEqual({
      name: 'Production',
      expired_time: now / 1000 + 90 * 24 * 60 * 60,
    })
  })

  it.each([
    ['in the past', -1, 'Choose an expiry in the future.'],
    ['beyond 90 days', 91, 'A key lasts at most 90 days.'],
  ])('refuses an expiry %s', (_label, days, message) => {
    const result = schema.safeParse({
      name: 'Production',
      expiresAt: daysFromNow(days, now),
    })

    expect(result.success).toBe(false)
    expect(result.error?.issues.map((issue) => issue.message)).toContain(
      message
    )
  })

  it('refuses a key without a name', () => {
    const result = schema.safeParse({
      name: '   ',
      expiresAt: daysFromNow(30, now),
    })

    expect(result.success).toBe(false)
  })
})

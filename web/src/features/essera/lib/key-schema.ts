import type { TFunction } from 'i18next'
import { z } from 'zod'

export const KEY_LIFETIME_DAYS = 90

const DAY_MS = 24 * 60 * 60 * 1000

export function daysFromNow(days: number, now = Date.now()): Date {
  return new Date(now + days * DAY_MS)
}

export function getKeySchema(t: TFunction, now = () => Date.now()) {
  return z.object({
    name: z.string().trim().min(1, t('Please enter a name')),
    expiresAt: z
      .date()
      .refine((date) => date.getTime() > now(), {
        message: t('Choose an expiry in the future.'),
      })
      .refine(
        (date) => date.getTime() <= now() + KEY_LIFETIME_DAYS * DAY_MS + 60_000,
        { message: t('A key lasts at most 90 days.') }
      ),
  })
}

export type KeyValues = z.infer<ReturnType<typeof getKeySchema>>

export function toKeyPayload(values: KeyValues) {
  return {
    name: values.name,
    expired_time: Math.floor(values.expiresAt.getTime() / 1000),
  }
}

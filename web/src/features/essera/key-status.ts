import type { StatusBadgeProps } from '@/components/status-badge'

type KeyStatus = Pick<StatusBadgeProps, 'variant'> & {
  label: string
  value: number
}

/**
 * A managed gateway's key states, as the gateway projects them from the Console's
 * configuration: active while the Console publishes the key, expired, or revoked once the
 * Console no longer publishes it. A person cannot pause a key, so there is no "disabled".
 * Labels are i18n keys.
 */
export const MANAGED_KEY_STATUSES: Record<number, KeyStatus> = {
  1: { label: 'Active', variant: 'success', value: 1 },
  3: { label: 'Expired', variant: 'warning', value: 3 },
  5: { label: 'Revoked', variant: 'danger', value: 5 },
}

export const MANAGED_KEY_STATUS_OPTIONS = Object.values(
  MANAGED_KEY_STATUSES
).map((status) => ({ label: status.label, value: String(status.value) }))

/**
 * The state to show for a managed key. The gateway updates it when a configuration applies,
 * so an active key whose expiry has passed since reads expired.
 */
export function managedKeyStatus(
  apiKey: { status: number; expired_time: number },
  now: number
): KeyStatus | undefined {
  const expired = apiKey.expired_time !== -1 && apiKey.expired_time * 1000 <= now
  if (apiKey.status === 1 && expired) return MANAGED_KEY_STATUSES[3]
  return MANAGED_KEY_STATUSES[apiKey.status]
}

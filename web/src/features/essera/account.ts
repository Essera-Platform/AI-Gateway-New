import { queryOptions, useQuery } from '@tanstack/react-query'

import { getSelf } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import type { EsseraAccount } from './types'
import { useEsseraManaged } from './use-essera-managed'

export type { EsseraAccount } from './types'

export const ESSERA_ACCOUNT_QUERY_KEY = ['essera', 'account'] as const

/** Reads the person's account; null for operators and users the Console does not know. */
export const esseraAccountQueryOptions = queryOptions({
  queryKey: ESSERA_ACCOUNT_QUERY_KEY,
  queryFn: async (): Promise<EsseraAccount | null> => {
    const response = await getSelf()
    return (response?.data?.essera as EsseraAccount | undefined) ?? null
  },
  staleTime: 60_000,
})

/**
 * The signed-in person's Console account in managed mode. Operators keep new-api's own
 * pages, so the account is not read for them.
 */
export function useEsseraAccount(): EsseraAccount | null {
  const { managed } = useEsseraManaged()
  const role = useAuthStore((s) => s.auth.user?.role)
  const enabled = managed && role !== undefined && role < ROLE.ADMIN
  const { data } = useQuery({ ...esseraAccountQueryOptions, enabled })
  return enabled ? (data ?? null) : null
}

/** An Essera admin: an ordinary gateway user who administers their own customer's people. */
export function isEsseraAdmin(
  role: number | undefined,
  account: EsseraAccount | null | undefined
): boolean {
  return role !== undefined && role < ROLE.ADMIN && account?.role === 'admin'
}

/** A Console amount, already in the customer's currency: "12.34 USD". */
export function formatSpend(amount: string, currency: string): string {
  return currency ? `${amount} ${currency}` : amount
}

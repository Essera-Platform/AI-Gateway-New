/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { QueryClient } from '@tanstack/react-query'
import { createFileRoute, redirect } from '@tanstack/react-router'
import z from 'zod'

import {
  esseraAccountQueryOptions,
  isEsseraAdmin,
  useEsseraAccount,
} from '@/features/essera/account'
import { EsseraPeople } from '@/features/essera/people'
import { Users } from '@/features/users'
import { ROLE } from '@/lib/roles'
import { statusQueryOptions } from '@/lib/status-query'
import { useAuthStore } from '@/stores/auth-store'

const usersSearchSchema = z.object({
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(undefined),
  filter: z.string().optional().catch(''),
  status: z
    .array(z.enum(['-1', '1', '2']))
    .optional()
    .catch([]),
  role: z
    .array(z.enum(['1', '10', '100']))
    .optional()
    .catch([]),
  group: z.array(z.string()).optional().catch([]),
})

// Modified by Essera, 2026-11: on a managed gateway an admin sees their customer's
// people from the Console; operators keep the users page.
async function isManagedEsseraAdmin(
  queryClient: QueryClient,
  role: number
): Promise<boolean> {
  try {
    const status = await queryClient.fetchQuery(statusQueryOptions)
    if (status?.essera_managed !== true) return false
    const account = await queryClient.fetchQuery(esseraAccountQueryOptions)
    return isEsseraAdmin(role, account)
  } catch {
    return false
  }
}

function UsersPage() {
  const role = useAuthStore((s) => s.auth.user?.role)
  const account = useEsseraAccount()
  return isEsseraAdmin(role, account) ? <EsseraPeople /> : <Users />
}

export const Route = createFileRoute('/_authenticated/users/')({
  beforeLoad: async ({ context }) => {
    const { auth } = useAuthStore.getState()

    if (auth.user && auth.user.role >= ROLE.ADMIN) return
    if (
      auth.user &&
      (await isManagedEsseraAdmin(context.queryClient, auth.user.role))
    ) {
      return
    }
    throw redirect({
      to: '/403',
    })
  },
  validateSearch: usersSearchSchema,
  component: UsersPage,
})

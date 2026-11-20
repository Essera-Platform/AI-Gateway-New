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
// Modified by Essera, 2026-11: the status filter accepts a managed key's revoked state.
import { createFileRoute } from '@tanstack/react-router'
import z from 'zod'

import { MANAGED_KEY_STATUS_OPTIONS } from '@/features/essera/key-status'
import { ApiKeys } from '@/features/keys'
import { API_KEY_STATUS_OPTIONS } from '@/features/keys/constants'

const statusValues = [
  ...new Set(
    [...API_KEY_STATUS_OPTIONS, ...MANAGED_KEY_STATUS_OPTIONS].map(
      (s) => s.value as `${number}`
    )
  ),
]

const apiKeySearchSchema = z.object({
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(undefined),
  status: z
    .array(z.enum(statusValues))
    .optional()
    .catch([]),
  filter: z.string().optional().catch(''),
  token: z.string().optional().catch(''),
})

export const Route = createFileRoute('/_authenticated/keys/')({
  validateSearch: apiKeySearchSchema,
  component: ApiKeys,
})

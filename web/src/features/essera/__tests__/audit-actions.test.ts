import { createInstance } from 'i18next'
import { expect, it } from 'vitest'

import type { AuditLog } from '@/features/usage-logs/audit/api'
import { buildAuditDetails } from '@/features/usage-logs/audit/lib/audit-details'

const t = (await (async () => {
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: {} })
  return i18n.t
})()) as Parameters<typeof buildAuditDetails>[1]

function consoleRow(
  action: string,
  content: string,
  newStatus?: string
): AuditLog {
  const params: Record<string, unknown> = { source: 'console' }
  if (newStatus) params.new_status = newStatus
  return {
    event_id: '0b8f8a52-63a4-4bd4-8c86-5f3a1f0d4e21',
    user_id: 0,
    username: 'owner@acme.example',
    actor_role: 0,
    created_at: 1795000000,
    category: 'operation',
    action,
    token_ref: '',
    ip: '',
    user_agent: '',
    method: '',
    route: '',
    status: 0,
    success: true,
    request_id: '',
    content,
    other: { op: { action, params } },
  }
}

it.each([
  ['credential.create', undefined, 'API key created'],
  ['credential.revoke', undefined, 'API key revoked'],
  ['end_user.create', undefined, 'User added'],
  ['end_user.update', 'suspended', 'User suspended'],
  ['end_user.update', 'active', 'User reactivated'],
  ['end_user.update', 'closed', 'User removed'],
  ['end_user.update', undefined, 'User changed'],
  ['gateway_access.remove', undefined, 'Sign-in removed'],
  ['person.sign_in', undefined, 'person.sign_in'],
])('names the Console action %s (%s) as %s', (action, status, label) => {
  const detail = buildAuditDetails(
    consoleRow(action, 'end_user e_1', status),
    t
  )
  expect(detail.summary).toBe(`${label} · end_user e_1`)
  expect(detail.fields).toEqual([])
})

it('names the action alone when the Console gives no resource', () => {
  expect(
    buildAuditDetails(consoleRow('credential.create', ''), t).summary
  ).toBe('API key created')
})

it('leaves the gateway’s own events as they were', () => {
  const own = consoleRow('vendor.sync', 'POST /api/vendors/sync')
  own.other = { op: { action: 'vendor.sync', params: {} } }
  expect(buildAuditDetails(own, t).summary).toBe('POST /api/vendors/sync')
})

import { api } from '@/lib/api'

import type { EsseraServiceStatus } from './types'

export type EsseraSignInStep = 'code' | 'new_password'

export interface EsseraSignInResponse {
  success: boolean
  message?: string
  data?: { next?: EsseraSignInStep; expires_in?: number } & Record<
    string,
    unknown
  >
}

export async function startEsseraSignIn(payload: {
  email: string
  password?: string
}): Promise<EsseraSignInResponse> {
  const res = await api.post<EsseraSignInResponse>(
    '/api/essera/sign-in',
    payload,
    { skipAuthRefresh: true, skipErrorHandler: true }
  )
  return res.data
}

export async function continueEsseraSignIn(
  payload: { code: string } | { new_password: string }
): Promise<EsseraSignInResponse> {
  const res = await api.post<EsseraSignInResponse>(
    '/api/essera/sign-in/continue',
    payload,
    { skipAuthRefresh: true, skipErrorHandler: true }
  )
  return res.data
}

export interface EsseraCreatedKey {
  id: number
  key_id: string
  key: string
  name: string
  expires_at: string
}

export async function createEsseraKey(payload: {
  name: string
  expired_time: number
}): Promise<{ success: boolean; message?: string; data?: EsseraCreatedKey }> {
  const res = await api.post('/api/token/', payload)
  return res.data
}

/** The status of the customer's providers, for its admin; unavailable when the Console is down. */
export async function getEsseraServiceStatus(): Promise<EsseraServiceStatus> {
  const res = await api.get<{
    success: boolean
    message?: string
    data?: EsseraServiceStatus
  }>('/api/essera/status', { skipErrorHandler: true })
  if (!res.data.success || !res.data.data) {
    throw new Error(res.data.message || 'Status temporarily unavailable')
  }
  return res.data.data
}

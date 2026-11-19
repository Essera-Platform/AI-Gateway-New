import { api } from '@/lib/api'

/** One of the tenant's people, as the Console lists them. */
export type EsseraPerson = {
  id: string
  email: string
  name?: string | null
  role: 'admin' | 'user'
  status: 'active' | 'suspended' | string
  external_id?: string | null
  key_count: number
  /** The signed-in admin's own row. */
  self?: boolean
  /** Changed at the Console; the Gateway follows at the next published configuration. */
  pending?: boolean
}

export type EsseraPeoplePage = {
  page: number
  page_size: number
  total: number
  items: EsseraPerson[]
}

export type EsseraPeopleResponse<T> = {
  success: boolean
  message?: string
  message_key?: string
  data?: T
}

/** An admin changes users only, never an admin or themselves. */
export function canChangePerson(person: EsseraPerson): boolean {
  return person.role === 'user' && !person.self
}

export type EsseraPersonAction = 'suspend' | 'reactivate' | 'remove'

export type NewEsseraPerson = {
  email: string
  name?: string
  external_id?: string
}

// The Gateway answers a refusal (400, 403, 404, 409) with a message for the person; a server
// failure is an error, and so is 401, so the session refresh still runs.
const answerRefusals = {
  validateStatus: (status: number) => status < 500 && status !== 401,
}

export async function getPeople(params: {
  p: number
  page_size: number
}): Promise<EsseraPeopleResponse<EsseraPeoplePage>> {
  const res = await api.get('/api/essera/people', {
    params,
    ...answerRefusals,
  })
  return res.data
}

export async function addPerson(
  person: NewEsseraPerson
): Promise<EsseraPeopleResponse<EsseraPerson>> {
  const res = await api.post('/api/essera/people', person, answerRefusals)
  return res.data
}

export async function changePerson(
  id: string,
  action: EsseraPersonAction
): Promise<EsseraPeopleResponse<EsseraPerson | { id: string; removed: true }>> {
  const path = `/api/essera/people/${encodeURIComponent(id)}`
  const res =
    action === 'remove'
      ? await api.delete(path, answerRefusals)
      : await api.post(`${path}/${action}`, undefined, answerRefusals)
  return res.data
}

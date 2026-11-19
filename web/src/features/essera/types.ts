/** The signed-in person's account as the Console reports it, on `/api/user/self`. */
export type EsseraAccount = {
  role: 'admin' | 'user'
  available: boolean
  customer_name?: string
  currency: string
  spend: string
  spend_limit: string | null
  spend_left?: string | null
  requests: number
  request_quota: number | null
  tokens: number
  token_quota: number | null
}

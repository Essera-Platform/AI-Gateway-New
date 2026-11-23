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
  /** This month's units by the name the Console's prices give them. */
  units?: Record<string, number> | null
}

export type EsseraProviderState =
  | 'operational'
  | 'degraded'
  | 'down'
  | 'maintenance'

/** How one provider the customer may use is doing, as the Console reports it. */
export type EsseraProviderStatus = {
  name: string
  region: string
  status: EsseraProviderState
  availability_last_hour: number | null
  p50_ms: number | null
  checked_at: string | null
  models: number
}

/** Gateway maintenance Essera announced in the Console. */
export type EsseraMaintenance = {
  status: 'scheduled' | 'in_progress'
  message: string
  starts_at: string | null
  ends_at: string | null
}

/** `/api/essera/status`: unavailable while the Console does not answer. */
export type EsseraServiceStatus =
  | { available: false }
  | {
      available: true
      providers: EsseraProviderStatus[]
      maintenance: EsseraMaintenance | null
    }

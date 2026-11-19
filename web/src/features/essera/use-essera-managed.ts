import { useStatus } from '@/hooks/use-status'

export type EsseraSignInMode = 'password' | 'email_otp'

export function useEsseraManaged(): {
  managed: boolean
  signInMode: EsseraSignInMode
} {
  const { status } = useStatus()
  return {
    managed: status?.essera_managed === true,
    signInMode:
      status?.essera_sign_in === 'email_otp' ? 'email_otp' : 'password',
  }
}

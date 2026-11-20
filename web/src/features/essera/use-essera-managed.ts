import { useStatus } from '@/hooks/use-status'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

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

/**
 * Whether the signed-in user is one of the Console's people on a managed gateway: anyone below
 * root there. Their account settings belong to Cognito and the Console, and the server closes
 * them; operators (root) keep new-api's.
 */
export function useEsseraPerson(): boolean {
  const { managed } = useEsseraManaged()
  const role = useAuthStore((state) => state.auth.user?.role ?? 0)
  return managed && role < ROLE.SUPER_ADMIN
}

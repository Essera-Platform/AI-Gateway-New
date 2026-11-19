import axios from 'axios'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { useAuthRedirect } from '@/features/auth/hooks/use-auth-redirect'
import { handleServerError } from '@/lib/handle-server-error'
import { AuthOperationError } from '@/lib/secure-verification'

import {
  continueEsseraSignIn,
  startEsseraSignIn,
  type EsseraSignInResponse,
  type EsseraSignInStep,
} from './api'
import { SignInCodeStep } from './components/sign-in-code-step'
import { SignInNewPasswordStep } from './components/sign-in-new-password-step'
import { SignInStartStep } from './components/sign-in-start-step'
import { useEsseraManaged } from './use-essera-managed'

type Step = 'start' | EsseraSignInStep

const SIGN_IN_FAILED = 'Login failed'
const CODE_EXPIRED = 'The code has expired. Send a new code.'
const DEFAULT_CODE_SECONDS = 180

// The server's answer names what went wrong without telling whether the account exists.
function signInError(error: unknown): AuthOperationError {
  if (axios.isAxiosError<{ message?: string }>(error)) {
    const message = error.response?.data?.message
    if (message) {
      return new AuthOperationError(message, undefined, { cause: error })
    }
  }
  return AuthOperationError.from(error, SIGN_IN_FAILED)
}

type EsseraSignInFormProps = {
  redirectTo?: string
}

export function EsseraSignInForm(props: EsseraSignInFormProps) {
  const { t } = useTranslation()
  const { signInMode } = useEsseraManaged()
  const { handleLoginResult } = useAuthRedirect()
  const [step, setStep] = useState<Step>('start')
  const [isLoading, setIsLoading] = useState(false)
  const [email, setEmail] = useState('')
  const [code, setCode] = useState({ round: 0, expiresIn: 0, expired: false })

  async function submit(request: () => Promise<EsseraSignInResponse>) {
    setIsLoading(true)
    try {
      const res = await request()
      const next = res.data?.next
      if (res.success && next) {
        if (next === 'code') {
          setCode((current) => ({
            round: current.round + 1,
            expiresIn: res.data?.expires_in || DEFAULT_CODE_SECONDS,
            expired: false,
          }))
        }
        setStep(next)
        return
      }
      if (res.success) {
        if (await handleLoginResult(res.data, props.redirectTo)) {
          toast.success(t('Welcome back!'))
        }
        return
      }
      handleServerError(new AuthOperationError(res.message || SIGN_IN_FAILED))
      if (next !== 'new_password') setStep('start')
    } catch (error: unknown) {
      const failure = signInError(error)
      const status = axios.isAxiosError(error) ? error.response?.status : 0
      if (step === 'code' && failure.message === CODE_EXPIRED) {
        setCode((current) => ({ ...current, expired: true }))
        return
      }
      handleServerError(failure)
      if (status !== 429) setStep('start')
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <div className='grid gap-4'>
      {step === 'start' && (
        <SignInStartStep
          mode={signInMode}
          isLoading={isLoading}
          onSubmit={(values) => {
            setEmail(values.email)
            return submit(() =>
              startEsseraSignIn(
                signInMode === 'password'
                  ? { email: values.email, password: values.password }
                  : { email: values.email }
              )
            )
          }}
        />
      )}
      {step === 'code' && (
        <SignInCodeStep
          key={code.round}
          email={email}
          expiresIn={code.expiresIn}
          expiredByServer={code.expired}
          canResend={signInMode === 'email_otp'}
          isLoading={isLoading}
          onSubmit={(values) =>
            submit(() => continueEsseraSignIn({ code: values.code }))
          }
          onResend={() => void submit(() => startEsseraSignIn({ email }))}
        />
      )}
      {step === 'new_password' && (
        <SignInNewPasswordStep
          isLoading={isLoading}
          onSubmit={(values) =>
            submit(() =>
              continueEsseraSignIn({ new_password: values.newPassword })
            )
          }
        />
      )}
      {step !== 'start' && (
        <Button
          type='button'
          variant='link'
          className='text-muted-foreground h-auto justify-self-start p-0 text-sm'
          disabled={isLoading}
          onClick={() => setStep('start')}
        >
          {t('Use a different email')}
        </Button>
      )}
    </div>
  )
}

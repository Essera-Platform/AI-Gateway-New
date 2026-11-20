import axios from 'axios'
import { ArrowLeft } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { useAuthRedirect } from '@/features/auth/hooks/use-auth-redirect'
import { useSystemConfig } from '@/hooks/use-system-config'
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
  const { systemName } = useSystemConfig()
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

  let heading = {
    title: t('Sign in to {{name}}', { name: systemName || 'Essera' }),
    description:
      signInMode === 'password'
        ? t('Use the email and password your administrator gave you.')
        : t('We will email you a one-time code. No password needed.'),
  }
  if (step === 'code') {
    heading = {
      title: t('Check your email'),
      description: t('Enter the code we sent to {{email}}.', { email }),
    }
  }
  if (step === 'new_password') {
    heading = {
      title: t('Set a new password'),
      description: t('Choose a new password for your account.'),
    }
  }

  return (
    <div className='grid gap-6'>
      <div className='grid gap-2'>
        {step !== 'start' && (
          <Button
            type='button'
            variant='link'
            className='text-primary h-auto gap-1.5 justify-self-start p-0 text-[13px] font-medium'
            disabled={isLoading}
            onClick={() => setStep('start')}
          >
            <ArrowLeft className='size-3.5' aria-hidden='true' />
            {t('Change email')}
          </Button>
        )}
        <h2 className='text-2xl font-bold tracking-tight'>{heading.title}</h2>
        <p className='text-muted-foreground text-sm'>{heading.description}</p>
      </div>
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
      <div className='border-border/70 grid justify-items-center gap-3 border-t pt-5 text-center'>
        <p className='text-muted-foreground text-xs leading-relaxed'>
          {t(
            "Access is managed by your organisation's administrator in the Essera Operations Console."
          )}
        </p>
        <span className='inline-flex items-center gap-1.5 rounded-full border border-emerald-200/60 bg-emerald-50/80 px-2.5 py-0.5 text-[11px] font-medium text-emerald-800 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300'>
          <span
            aria-hidden='true'
            className='size-1.5 rounded-full bg-emerald-500 motion-safe:animate-pulse'
          />
          {t('Managed by the Essera Operations Console')}
        </span>
      </div>
    </div>
  )
}

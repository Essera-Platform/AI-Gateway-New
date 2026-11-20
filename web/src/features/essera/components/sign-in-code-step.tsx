import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import {
  InputOTP,
  InputOTPGroup,
  InputOTPSlot,
} from '@/components/ui/input-otp'
import { useCountdown } from '@/hooks/use-countdown'

import { getCodeSchema, type CodeValues } from '../lib/sign-in-schema'
import { FIELD_LABEL, SignInSubmitButton } from './sign-in-submit-button'

export const RESEND_COOLDOWN_SECONDS = 30
// The schema takes 6 to 8 digits; passwordless codes are 8, so 8 boxes fit both.
const CODE_LENGTH = 8
const CODE_SLOTS = Array.from({ length: CODE_LENGTH }, (_, index) => index)

function clock(seconds: number): string {
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
}

type SignInCodeStepProps = {
  expiresIn: number
  expiredByServer: boolean
  canResend: boolean
  isLoading: boolean
  onSubmit: (values: CodeValues) => Promise<void>
  onResend: () => void
}

export function SignInCodeStep(props: SignInCodeStepProps) {
  const { t } = useTranslation()
  const schema = useMemo(() => getCodeSchema(t), [t])
  const form = useForm<CodeValues>({
    resolver: zodResolver(schema),
    defaultValues: { code: '' },
  })
  const expiry = useCountdown({
    initialSeconds: props.expiresIn,
    autoStart: true,
  })
  const cooldown = useCountdown({
    initialSeconds: RESEND_COOLDOWN_SECONDS,
    autoStart: true,
  })
  const startExpiry = expiry.start
  const startCooldown = cooldown.start
  useEffect(() => {
    startExpiry()
    startCooldown()
  }, [startExpiry, startCooldown])

  const expired = props.expiredByServer || !expiry.isActive

  // A code is spent by the first attempt, so a complete code and a click on Verify
  // (or a second click) never send it twice; a failed attempt frees the form again.
  const inFlight = useRef(false)
  const [submitting, setSubmitting] = useState(false)
  const submitOnce = async (values: CodeValues) => {
    if (inFlight.current) return
    inFlight.current = true
    setSubmitting(true)
    try {
      await props.onSubmit(values)
    } finally {
      inFlight.current = false
      setSubmitting(false)
    }
  }
  const onSubmit = form.handleSubmit(submitOnce)

  return (
    <Form {...form}>
      <form onSubmit={onSubmit} className='grid gap-4'>
        <FormField
          control={form.control}
          name='code'
          render={({ field }) => (
            <FormItem>
              <FormLabel className={FIELD_LABEL}>
                {t('Verification code')}
              </FormLabel>
              <FormControl>
                <InputOTP
                  maxLength={CODE_LENGTH}
                  inputMode='numeric'
                  pattern='^[0-9]*$'
                  autoComplete='one-time-code'
                  autoFocus
                  containerClassName='w-full'
                  value={field.value}
                  onChange={field.onChange}
                  onComplete={() => {
                    if (!expired) void onSubmit()
                  }}
                  onBlur={field.onBlur}
                  name={field.name}
                  ref={field.ref}
                >
                  <InputOTPGroup className='grid w-full grid-cols-8 gap-1.5'>
                    {CODE_SLOTS.map((index) => (
                      <InputOTPSlot
                        key={index}
                        index={index}
                        className='num bg-card dark:bg-card h-12 w-full rounded-lg border text-xl font-semibold tracking-normal first:rounded-lg last:rounded-lg data-[active=true]:border-emerald-500 data-[active=true]:ring-emerald-500/20'
                      />
                    ))}
                  </InputOTPGroup>
                </InputOTP>
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <SignInSubmitButton
          isLoading={props.isLoading || submitting}
          disabled={expired}
          label={t('Verify and sign in')}
        />
        <div className='flex flex-wrap items-center justify-between gap-2 text-[13px]'>
          {expired ? (
            <p role='alert' className='text-destructive'>
              {t('The code has expired. Send a new code.')}
            </p>
          ) : (
            <p role='timer' className='text-muted-foreground'>
              {t('Code expires in')}{' '}
              <span className='num text-primary font-semibold'>
                {clock(expiry.secondsLeft)}
              </span>
            </p>
          )}
          {props.canResend && (
            <Button
              type='button'
              variant='link'
              className='text-primary disabled:text-muted-foreground h-auto p-0 text-[13px] font-medium disabled:opacity-100'
              disabled={props.isLoading || cooldown.isActive}
              onClick={props.onResend}
            >
              {cooldown.isActive
                ? t('Send a new code in {{seconds}} s', {
                    seconds: cooldown.secondsLeft,
                  })
                : t('Send a new code')}
            </Button>
          )}
        </div>
      </form>
    </Form>
  )
}

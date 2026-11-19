import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useMemo } from 'react'
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
import { Input } from '@/components/ui/input'
import { useCountdown } from '@/hooks/use-countdown'

import { getCodeSchema, type CodeValues } from '../lib/sign-in-schema'
import { SignInSubmitButton } from './sign-in-submit-button'

export const RESEND_COOLDOWN_SECONDS = 30

function clock(seconds: number): string {
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
}

type SignInCodeStepProps = {
  email: string
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

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(props.onSubmit)} className='grid gap-4'>
        <p className='text-muted-foreground text-sm'>
          {t('Enter the code we sent to {{email}}.', { email: props.email })}
        </p>
        <FormField
          control={form.control}
          name='code'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Verification code')}</FormLabel>
              <FormControl>
                <Input
                  autoComplete='one-time-code'
                  inputMode='numeric'
                  maxLength={8}
                  autoFocus
                  placeholder={t('Enter the verification code')}
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        {expired ? (
          <p role='alert' className='text-destructive text-sm'>
            {t('The code has expired. Send a new code.')}
          </p>
        ) : (
          <p role='timer' className='text-muted-foreground text-sm'>
            {t('The code expires in {{time}}.', {
              time: clock(expiry.secondsLeft),
            })}
          </p>
        )}
        <SignInSubmitButton
          isLoading={props.isLoading}
          disabled={expired}
          label={t('Continue')}
        />
        {props.canResend && (
          <Button
            type='button'
            variant='outline'
            className='w-full'
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
      </form>
    </Form>
  )
}

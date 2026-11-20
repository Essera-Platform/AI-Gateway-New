import { zodResolver } from '@hookform/resolvers/zod'
import { Mail } from 'lucide-react'
import { useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { PasswordInput } from '@/components/password-input'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import { getStartSchema, type StartValues } from '../lib/sign-in-schema'
import type { EsseraSignInMode } from '../use-essera-managed'
import { FIELD_LABEL, SignInSubmitButton } from './sign-in-submit-button'

export function SignInStartStep(props: {
  mode: EsseraSignInMode
  isLoading: boolean
  onSubmit: (values: StartValues) => Promise<void>
}) {
  const { t } = useTranslation()
  const schema = useMemo(() => getStartSchema(t, props.mode), [t, props.mode])
  const form = useForm<StartValues>({
    resolver: zodResolver(schema),
    defaultValues: { email: '', password: '' },
  })

  return (
    <Form {...form}>
      <form
        onSubmit={form.handleSubmit(async (values) => {
          await props.onSubmit(values)
          form.setValue('password', '')
        })}
        className='grid gap-4'
      >
        <FormField
          control={form.control}
          name='email'
          render={({ field }) => (
            <FormItem>
              <FormLabel className={FIELD_LABEL}>{t('Email')}</FormLabel>
              <div className='relative'>
                <Mail
                  aria-hidden='true'
                  className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2'
                />
                <FormControl>
                  <Input
                    type='email'
                    autoComplete='username'
                    autoFocus
                    placeholder='name@company.com'
                    className='h-11 pl-9'
                    {...field}
                  />
                </FormControl>
              </div>
              <FormMessage />
            </FormItem>
          )}
        />
        {props.mode === 'password' && (
          <FormField
            control={form.control}
            name='password'
            render={({ field }) => (
              <FormItem>
                <FormLabel className={FIELD_LABEL}>{t('Password')}</FormLabel>
                <FormControl>
                  <PasswordInput
                    autoComplete='current-password'
                    placeholder={t('Enter password')}
                    {...field}
                  />
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
        )}
        <SignInSubmitButton
          isLoading={props.isLoading}
          label={props.mode === 'password' ? t('Sign in') : t('Continue')}
        />
      </form>
    </Form>
  )
}

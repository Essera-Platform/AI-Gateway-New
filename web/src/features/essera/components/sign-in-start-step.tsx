import { zodResolver } from '@hookform/resolvers/zod'
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
import { SignInSubmitButton } from './sign-in-submit-button'

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
              <FormLabel>{t('Email')}</FormLabel>
              <FormControl>
                <Input
                  type='email'
                  autoComplete='username'
                  placeholder={t('Enter your email')}
                  {...field}
                />
              </FormControl>
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
                <FormLabel>{t('Password')}</FormLabel>
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

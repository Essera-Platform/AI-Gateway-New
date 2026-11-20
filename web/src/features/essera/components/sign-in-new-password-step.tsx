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

import {
  getNewPasswordSchema,
  type NewPasswordValues,
} from '../lib/sign-in-schema'
import { FIELD_LABEL, SignInSubmitButton } from './sign-in-submit-button'

export function SignInNewPasswordStep(props: {
  isLoading: boolean
  onSubmit: (values: NewPasswordValues) => Promise<void>
}) {
  const { t } = useTranslation()
  const schema = useMemo(() => getNewPasswordSchema(t), [t])
  const form = useForm<NewPasswordValues>({
    resolver: zodResolver(schema),
    defaultValues: { newPassword: '', confirmPassword: '' },
  })

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(props.onSubmit)} className='grid gap-4'>
        <FormField
          control={form.control}
          name='newPassword'
          render={({ field }) => (
            <FormItem>
              <FormLabel className={FIELD_LABEL}>{t('New password')}</FormLabel>
              <FormControl>
                <PasswordInput
                  autoComplete='new-password'
                  autoFocus
                  {...field}
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name='confirmPassword'
          render={({ field }) => (
            <FormItem>
              <FormLabel className={FIELD_LABEL}>
                {t('Confirm new password')}
              </FormLabel>
              <FormControl>
                <PasswordInput autoComplete='new-password' {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <SignInSubmitButton isLoading={props.isLoading} label={t('Continue')} />
      </form>
    </Form>
  )
}

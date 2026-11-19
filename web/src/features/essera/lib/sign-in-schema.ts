import type { TFunction } from 'i18next'
import { z } from 'zod'

import type { EsseraSignInMode } from '../use-essera-managed'

export function getStartSchema(t: TFunction, mode: EsseraSignInMode) {
  return z.object({
    email: z.string().trim().email(t('Please enter a valid email address')),
    password:
      mode === 'password'
        ? z.string().min(1, t('Please enter your password'))
        : z.string(),
  })
}

// Cognito sends 6-digit codes for MFA and 8-digit codes for passwordless sign-in.
export function getCodeSchema(t: TFunction) {
  return z.object({
    code: z
      .string()
      .trim()
      .regex(/^\d{6,8}$/, t('Enter the code from the email.')),
  })
}

export function getNewPasswordSchema(t: TFunction) {
  return z
    .object({
      newPassword: z
        .string()
        .min(8, t('Password must be at least 8 characters')),
      confirmPassword: z.string(),
    })
    .refine((values) => values.newPassword === values.confirmPassword, {
      message: t('Passwords do not match'),
      path: ['confirmPassword'],
    })
}

export type StartValues = z.infer<ReturnType<typeof getStartSchema>>
export type CodeValues = z.infer<ReturnType<typeof getCodeSchema>>
export type NewPasswordValues = z.infer<ReturnType<typeof getNewPasswordSchema>>

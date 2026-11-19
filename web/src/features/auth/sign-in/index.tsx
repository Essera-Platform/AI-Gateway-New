/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
// Modified by Essera, 2026-11: on a managed gateway people sign in with the Essera form; operators keep this one.
import { Link, useSearch } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { EsseraSignInForm } from '@/features/essera/sign-in-form'
import { useEsseraManaged } from '@/features/essera/use-essera-managed'
import { useStatus } from '@/hooks/use-status'

import { AuthLayout } from '../auth-layout'
import { TermsFooter } from '../components/terms-footer'
import { UserAuthForm } from './components/user-auth-form'

export function SignIn() {
  const { t } = useTranslation()
  const { redirect } = useSearch({ from: '/(auth)/sign-in' })
  const { status } = useStatus()
  const { managed } = useEsseraManaged()
  const [operatorSignIn, setOperatorSignIn] = useState(false)

  return (
    <AuthLayout>
      <div className='w-full space-y-8'>
        <div className='space-y-2'>
          <h2 className='text-center text-2xl font-semibold tracking-tight sm:text-left'>
            {t('Sign in')}
          </h2>
          {!managed &&
            !status?.self_use_mode_enabled &&
            status?.register_enabled !== false && (
              <p className='text-muted-foreground text-left text-sm sm:text-base'>
                {t("Don't have an account?")}{' '}
                <Link
                  to='/sign-up'
                  className='hover:text-primary font-medium underline underline-offset-4'
                >
                  {t('Sign up')}
                </Link>
                .
              </p>
            )}
        </div>

        {managed && !operatorSignIn ? (
          <EsseraSignInForm
            redirectTo={redirect}
            onOperatorSignIn={() => setOperatorSignIn(true)}
          />
        ) : (
          <UserAuthForm redirectTo={redirect} />
        )}
        {managed && operatorSignIn && (
          <Button
            type='button'
            variant='link'
            className='text-muted-foreground h-auto p-0 text-sm'
            onClick={() => setOperatorSignIn(false)}
          >
            {t('Back to Essera sign-in')}
          </Button>
        )}

        <TermsFooter
          variant='sign-in'
          status={status}
          className='text-center'
        />
      </div>
    </AuthLayout>
  )
}

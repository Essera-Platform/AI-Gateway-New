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
// Modified by Essera, 2026-11: the Essera sign-in look: a header with the Essera logo on a managed gateway and a Light/Dark switch, the grid backdrop, and the form on a card.
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { ThemeSwitch } from '@/components/theme-switch'
import { Skeleton } from '@/components/ui/skeleton'
import { EsseraLogo } from '@/features/essera/brand/essera-logo'
import { useStatus } from '@/hooks/use-status'
import { useSystemConfig } from '@/hooks/use-system-config'

type AuthLayoutProps = {
  children: React.ReactNode
}

export function AuthLayout({ children }: AuthLayoutProps) {
  const { t } = useTranslation()
  const { systemName, logo, loading } = useSystemConfig()
  const { status } = useStatus()
  const managed = status?.essera_managed === true

  let brand = (
    <>
      <div className='relative h-8 w-8'>
        {loading ? (
          <Skeleton className='absolute inset-0 rounded-full' />
        ) : (
          <img
            src={logo}
            alt={t('Logo')}
            className='h-8 w-8 rounded-full object-cover'
          />
        )}
      </div>
      {loading ? (
        <Skeleton className='h-6 w-24' />
      ) : (
        <h1 className='text-xl font-medium'>{systemName}</h1>
      )}
    </>
  )
  if (managed) brand = <EsseraLogo />

  return (
    <div className='bg-marketing relative flex min-h-svh flex-col'>
      <div
        aria-hidden='true'
        className='essera-grid-bg pointer-events-none fixed inset-0 opacity-70'
      />
      <header className='bg-card/70 relative z-10 flex h-16 items-center justify-between border-b px-4 backdrop-blur-sm sm:px-8'>
        <Link
          to='/'
          aria-label={systemName}
          className='flex items-center gap-2 transition-opacity hover:opacity-80'
        >
          {brand}
        </Link>
        <ThemeSwitch />
      </header>
      <main className='relative z-10 flex flex-1 items-center justify-center px-4 py-10'>
        <div className='bg-card w-full max-w-[440px] space-y-2 rounded-2xl border p-6 shadow-[0_12px_30px_-10px_rgb(15_23_42/0.08)] sm:p-8'>
          {children}
        </div>
      </main>
    </div>
  )
}

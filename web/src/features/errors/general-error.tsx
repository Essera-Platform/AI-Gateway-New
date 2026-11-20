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
// Modified by Essera, 2026-11: the designs' error page (ErrorPage); a managed gateway does not send people to new-api's issue tracker.
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { ErrorPage } from '@/features/essera/error-page'
import { cachedStatusIsManaged } from '@/features/essera/language'
import { cn } from '@/lib/utils'

const FEEDBACK_URL = 'https://github.com/QuantumNous/new-api/issues'

type GeneralErrorProps = React.HTMLAttributes<HTMLDivElement> & {
  minimal?: boolean
  error?: unknown
}

function getHttpStatus(error: unknown): number | undefined {
  if (typeof error !== 'object' || error === null) return undefined
  const response = (error as Record<string, unknown>).response
  if (typeof response !== 'object' || response === null) return undefined
  const status = (response as Record<string, unknown>).status
  return typeof status === 'number' ? status : undefined
}

export function GeneralError({
  className,
  minimal = false,
  error,
}: GeneralErrorProps) {
  const { t } = useTranslation()
  // Read from the saved status: an error page must not depend on the query client.
  const managed = cachedStatusIsManaged()
  useEffect(() => {
    if (
      !(error instanceof Error) ||
      (error.name !== 'ChunkLoadError' &&
        !/^Loading (CSS )?chunk .+ failed\./.test(error.message))
    ) {
      return
    }

    // Recover only when the failed route is displayed, never on intent preload.
    // Keep the marker across reloads so a persistent failure shows this page.
    try {
      const key = `tanstack_router_reload:${error.message}`
      if (sessionStorage.getItem(key)) return
      sessionStorage.setItem(key, '1')
    } catch {
      // Without a persistent marker, reloading could loop indefinitely.
      return
    }
    window.location.reload()
  }, [error])

  const status = getHttpStatus(error)
  const isRateLimited = status === 429
  const title = isRateLimited
    ? t('Too many requests')
    : `${t('Oops! Something went wrong')} ${`:')`}`
  const description = isRateLimited
    ? t('Please wait a moment before trying again.')
    : t('Please try again later.')

  if (minimal) {
    return (
      <div className={cn('h-svh w-full', className)}>
        <div className='m-auto flex h-full w-full flex-col items-center justify-center gap-2'>
          <span className='font-medium'>{title}</span>
          <p className='text-muted-foreground text-center'>
            {t('We apologize for the inconvenience.')} <br /> {description}
          </p>
        </div>
      </div>
    )
  }

  return (
    <ErrorPage
      status={status}
      className={className}
      title={title}
      description={
        <>
          <p>
            {t('We apologize for the inconvenience.')} {description}
          </p>
          {!managed && (
            <p className='mt-2 text-xs'>
              {t('If this keeps happening, please report it on GitHub Issues.')}
            </p>
          )}
        </>
      }
      actions={
        !managed && (
          <Button
            variant='outline'
            render={
              <a
                href={FEEDBACK_URL}
                target='_blank'
                rel='noopener noreferrer'
              />
            }
          >
            {t('Report an issue')}
          </Button>
        )
      }
    />
  )
}

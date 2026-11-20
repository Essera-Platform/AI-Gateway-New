import { useNavigate, useRouter } from '@tanstack/react-router'
import { ArrowLeft, Home, LayoutGrid } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

type ErrorPageProps = {
  /** The HTTP status, shown as the page's heading; omitted for an error without one. */
  status?: number
  title: ReactNode
  description: ReactNode
  /** Extra actions, placed before the standard ones. */
  actions?: ReactNode
  /** Hide the standard "Go Back", Overview or Home and Model Square actions. */
  hideNavigation?: boolean
  className?: string
}

/**
 * The designs' error page: a card on the grid backdrop with a mono "HTTP <status>" tag, the
 * status as a large mono figure with an emerald dot, a title, a line of text and the ways out:
 * back, Overview for someone signed in (Home otherwise), and the Model Square.
 */
export function ErrorPage(props: ErrorPageProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { history } = useRouter()
  const signedIn = useAuthStore((state) => state.auth.user !== null)

  return (
    <div
      className={cn(
        'bg-background relative flex h-svh w-full items-center justify-center overflow-hidden px-4',
        props.className
      )}
    >
      <div
        aria-hidden='true'
        className='essera-grid-bg pointer-events-none absolute inset-0 opacity-60'
      />
      <div className='bg-card shadow-card relative w-full max-w-lg rounded-2xl border px-6 py-10 text-center sm:px-10'>
        {props.status !== undefined && (
          <>
            <span className='bg-muted text-muted-foreground inline-flex rounded px-2 py-0.5 font-mono text-[11px] font-semibold tracking-widest uppercase'>
              HTTP {props.status}
            </span>
            <h1 className='num text-foreground mt-4 text-7xl leading-none font-extrabold sm:text-8xl'>
              {props.status}
              <span
                aria-hidden='true'
                className='bg-unit ml-1 inline-block size-3.5 rounded-full sm:size-4'
              />
            </h1>
          </>
        )}
        <p className='text-foreground mt-5 text-xl font-bold tracking-tight'>
          {props.title}
        </p>
        <div className='text-muted-foreground mx-auto mt-2 max-w-sm text-sm leading-relaxed'>
          {props.description}
        </div>
        <div className='mt-8 flex flex-wrap items-center justify-center gap-3'>
          {props.actions}
          {!props.hideNavigation && (
            <>
              {signedIn ? (
                <Button
                  onClick={() =>
                    navigate({
                      to: '/dashboard/$section',
                      params: { section: 'overview' },
                    })
                  }
                >
                  <Home aria-hidden='true' />
                  {t('Go to Overview')}
                </Button>
              ) : (
                <Button onClick={() => navigate({ to: '/' })}>
                  <Home aria-hidden='true' />
                  {t('Back to Home')}
                </Button>
              )}
              <Button
                variant='outline'
                onClick={() => navigate({ to: '/pricing' })}
              >
                <LayoutGrid aria-hidden='true' />
                {t('Open Model Square')}
              </Button>
            </>
          )}
        </div>
        {!props.hideNavigation && (
          <Button
            variant='link'
            className='text-muted-foreground mt-4 h-auto p-0 text-[13px]'
            onClick={() => history.go(-1)}
          >
            <ArrowLeft aria-hidden='true' className='size-3.5' />
            {t('Go Back')}
          </Button>
        )}
      </div>
    </div>
  )
}

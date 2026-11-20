import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

/**
 * The Essera mark: an emerald tile with a bold white "E", the mark of the Essera Operations
 * Console too, so the two products read as one brand. A real logo replaces this file and
 * `public/essera-favicon.svg`.
 */
export function EsseraMark(props: { className?: string }) {
  return (
    <svg
      viewBox='0 0 32 32'
      aria-hidden='true'
      className={cn('size-8 shrink-0', props.className)}
    >
      <rect width='32' height='32' rx='8' fill='#059669' />
      <path
        d='M10.5 8.5h11v3.2h-7.2v2.7h6.2v3.2h-6.2v2.7h7.2v3.2h-11z'
        fill='#fff'
      />
    </svg>
  )
}

type EsseraLogoProps = {
  className?: string
  markClassName?: string
  /** Only the tile, with the name for screen readers. */
  markOnly?: boolean
}

/** The mark with the wordmark: "ESSERA" over "AI Gateway". */
export function EsseraLogo(props: EsseraLogoProps) {
  const { t } = useTranslation()
  return (
    <span
      data-slot='essera-logo'
      className={cn(
        'inline-flex min-w-0 items-center gap-2.5',
        props.className
      )}
    >
      <EsseraMark className={props.markClassName} />
      <span
        className={cn(
          'flex min-w-0 flex-col leading-none',
          props.markOnly && 'sr-only'
        )}
      >
        <span className='text-foreground text-[15px] font-extrabold tracking-[0.12em] uppercase'>
          Essera
        </span>
        <span className='text-muted-foreground mt-1 text-[10px] font-semibold tracking-[0.14em] whitespace-nowrap uppercase'>
          {t('AI Gateway')}
        </span>
      </span>
    </span>
  )
}

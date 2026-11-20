import { useTranslation } from 'react-i18next'

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { useStatus } from '@/hooks/use-status'

/**
 * Says, on every admin page, that this gateway is administered by the Essera Operations
 * Console: what the Console publishes is refused here (409), so the reason is visible before
 * an edit fails.
 */
export function EsseraManagedBadge() {
  const { t } = useTranslation()
  const { status } = useStatus()
  if (status?.essera_managed !== true) return null

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span
            tabIndex={0}
            className='hidden shrink-0 items-center gap-1.5 rounded-full border border-emerald-200/60 bg-emerald-50/80 px-2.5 py-0.5 text-[11px] font-medium whitespace-nowrap text-emerald-800 outline-none focus-visible:ring-2 focus-visible:ring-emerald-500/30 md:inline-flex dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300'
          />
        }
      >
        <span
          aria-hidden='true'
          className='size-1.5 rounded-full bg-emerald-500 motion-safe:animate-pulse'
        />
        {t('Managed by the Essera Operations Console')}
      </TooltipTrigger>
      <TooltipContent className='max-w-xs'>
        {t(
          'Providers, customers, keys, models and prices are changed in the Essera Operations Console; edits to them here are refused.'
        )}
      </TooltipContent>
    </Tooltip>
  )
}

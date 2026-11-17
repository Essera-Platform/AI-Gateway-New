import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
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
        render={<Badge variant='outline' tabIndex={0} className='ms-1' />}
      >
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

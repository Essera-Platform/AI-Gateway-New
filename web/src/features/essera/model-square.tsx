import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { esseraPriceParts, type EsseraModelInfo } from './model-square-format'

/**
 * The person's prices for a model, or, for a guest, that prices follow sign-in. Laid out
 * like the Model Square's own price summary.
 */
export function EsseraModelPrices(props: {
  info: EsseraModelInfo
  compact?: boolean
}) {
  const { t } = useTranslation()
  if (!props.info.signedIn) {
    return (
      <span className='text-muted-foreground col-span-full text-xs'>
        {t('Sign in to see prices')}
      </span>
    )
  }
  if (!props.info.pricesAvailable) {
    return (
      <span className='text-muted-foreground col-span-full text-xs'>
        {t('Prices are temporarily unavailable')}
      </span>
    )
  }
  if (props.info.prices.length === 0) {
    return (
      <span className='text-muted-foreground col-span-full text-xs'>
        {t('No price available')}
      </span>
    )
  }
  return (
    <>
      {props.info.prices.map((price) => {
        const parts = esseraPriceParts(t, price)
        return (
          <div
            key={price.unit}
            className={cn(
              'flex min-w-0 gap-1',
              props.compact ? 'items-baseline' : 'flex-col'
            )}
          >
            <span className='text-muted-foreground text-xs'>{parts.label}</span>
            <span className='font-mono text-sm font-semibold tabular-nums'>
              {parts.value}
              <span className='text-muted-foreground text-xs font-normal'>
                {' '}
                / {parts.per}
              </span>
            </span>
          </div>
        )
      })}
    </>
  )
}

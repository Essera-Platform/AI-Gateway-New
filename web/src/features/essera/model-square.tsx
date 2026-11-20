import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import { NumberValue } from './brand/number-value'
import {
  esseraPriceParts,
  inBillOrder,
  isLongContextUnit,
  type EsseraModelInfo,
} from './model-square-format'

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
  const hasLongContext = props.info.prices.some((price) =>
    isLongContextUnit(price.unit)
  )
  return (
    <>
      {inBillOrder(props.info.prices).map((price) => {
        const parts = esseraPriceParts(t, price)
        return (
          <div
            key={price.unit}
            className={cn(
              'flex min-w-0 gap-1',
              props.compact
                ? 'items-baseline'
                : 'col-span-full items-baseline justify-between gap-3'
            )}
          >
            <span className='text-muted-foreground font-mono text-xs'>
              {parts.label}
            </span>
            <span className='text-[13px] font-semibold whitespace-nowrap'>
              <NumberValue value={parts.value} className='tracking-normal' />
              <span className='text-muted-foreground font-mono text-xs font-normal'>
                {' '}
                / {parts.per}
              </span>
            </span>
          </div>
        )
      })}
      {hasLongContext && !props.compact && (
        <span className='text-muted-foreground col-span-full text-xs'>
          {t(
            'Long context prices apply to every token of a request whose prompt is longer than the model’s long-context size.'
          )}
        </span>
      )}
    </>
  )
}

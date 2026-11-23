import { ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import type { PricingModel } from '@/features/pricing/types'

import { EsseraModelMetricsLine } from './model-metrics'
import { EsseraModelPrices } from './model-square'
import {
  esseraModelTypeLabel,
  type EsseraModelInfo,
} from './model-square-format'

/**
 * A model on a managed gateway's Model Square, in the Essera card design: the display name,
 * the model id to copy, its type, the person's prices right under the name and the Console's
 * measurements of it at the bottom, so cards of a row with fewer prices leave the gap above
 * the measurements, not above the prices.
 */
export function EsseraModelCard(props: {
  model: PricingModel
  info: EsseraModelInfo
  onClick: (modelName: string) => void
}) {
  const { t } = useTranslation()
  const modelName = props.model.model_name || ''
  const displayName = props.info.displayName || modelName
  const vendor = props.model.vendor_name

  return (
    <article className='bg-card shadow-card hover:border-primary/40 flex h-full min-w-0 flex-col gap-4 rounded-xl border p-5 transition-[border-color,box-shadow] hover:shadow-[0_8px_24px_-4px_color-mix(in_oklch,var(--primary)_10%,transparent)]'>
      <div className='flex items-start justify-between gap-3'>
        <span
          aria-hidden='true'
          className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg text-sm font-bold'
        >
          {displayName.charAt(0).toUpperCase() || '?'}
        </span>
        <div className='flex min-w-0 items-center gap-2'>
          {vendor && (
            <span
              className='text-muted-foreground truncate font-mono text-[11px]'
              title={vendor}
            >
              {vendor}
            </span>
          )}
          {props.info.modelType && (
            <span className='bg-muted text-muted-foreground shrink-0 rounded-md px-2 py-0.5 text-[11px] font-medium'>
              {esseraModelTypeLabel(t, props.info.modelType)}
            </span>
          )}
        </div>
      </div>

      <div className='min-w-0'>
        <h3
          className='truncate text-base font-bold tracking-tight'
          title={displayName}
        >
          {displayName}
        </h3>
        <div className='mt-1 flex min-w-0 items-center gap-1'>
          <code
            className='text-muted-foreground truncate font-mono text-xs'
            title={modelName}
          >
            {modelName}
          </code>
          <CopyButton
            value={modelName}
            tooltip={t('Copy model name')}
            className='size-6 shrink-0'
            iconClassName='size-3'
          />
        </div>
      </div>

      <div
        role='group'
        aria-label={t('Pricing')}
        className='bg-muted/40 grid gap-1.5 rounded-lg px-3 py-2.5'
      >
        <EsseraModelPrices info={props.info} />
      </div>

      <EsseraModelMetricsLine
        metrics={props.info.metrics}
        className='border-border/60 mt-auto border-t pt-2'
      >
        {props.info.signedIn && (
          <Button
            variant='ghost'
            size='sm'
            className='text-primary hover:text-primary'
            onClick={() => props.onClick(modelName)}
          >
            {t('Details')}
            <ChevronRight aria-hidden className='size-3.5' />
          </Button>
        )}
      </EsseraModelMetricsLine>
    </article>
  )
}

import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  formatLatency,
  formatThroughput,
  formatUptimePct,
  getSuccessRateTextClass,
} from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

import { checkedAgo, type EsseraModelMetrics } from './model-square-format'

function figure(
  value: number | null,
  format: (value: number) => string
): string {
  return value == null ? '—' : format(value)
}

/**
 * A model's availability, time to first token and output speed over the last 24 hours, as the
 * Console measured them. A model the Console answered no figures for has had no requests yet;
 * a guest or a Console that does not answer gets nothing at all.
 */
export function EsseraModelMetricsLine(props: {
  metrics: EsseraModelMetrics | undefined
  metricsAvailable?: boolean
  className?: string
  children?: ReactNode
}) {
  const { t } = useTranslation()
  const metrics = props.metrics
  const checked = metrics ? checkedAgo(t, metrics.checked_at) : null
  return (
    <div
      className={cn(
        'flex w-full min-w-0 items-center justify-between gap-3',
        props.className
      )}
    >
      {metrics ? (
        <div className='min-w-0'>
          <dl
            aria-label={t('Performance metrics for the last 24 hours')}
            className='flex min-w-0 items-start gap-5 text-xs tabular-nums'
          >
            <div
              title={t(
                'Requests that succeeded, leaving out those the caller caused'
              )}
              className='shrink-0'
            >
              <dt className='text-muted-foreground text-[11px] leading-4'>
                {t('Availability')}
              </dt>
              <dd
                className={cn(
                  'mt-1 font-mono whitespace-nowrap',
                  metrics.availability_24h != null &&
                    getSuccessRateTextClass(metrics.availability_24h)
                )}
              >
                {figure(metrics.availability_24h, formatUptimePct)}
              </dd>
            </div>
            <div title={t('Median time to first token')} className='shrink-0'>
              <dt className='text-muted-foreground text-[11px] leading-4'>
                {t('TTFT')}
              </dt>
              <dd className='mt-1 font-mono whitespace-nowrap'>
                {figure(metrics.p50_ttft_ms, formatLatency)}
              </dd>
            </div>
            <div title={t('Output tokens per second')} className='shrink-0'>
              <dt className='text-muted-foreground text-[11px] leading-4'>
                {t('Throughput short')}
              </dt>
              <dd className='mt-1 font-mono whitespace-nowrap'>
                {figure(metrics.output_per_second, formatThroughput)}
              </dd>
            </div>
          </dl>
          {checked && (
            <p className='text-muted-foreground mt-1 text-[11px] leading-4'>
              {checked}
            </p>
          )}
        </div>
      ) : (
        <NoMetrics shown={props.metricsAvailable === true} />
      )}
      {props.children}
    </div>
  )
}

function NoMetrics(props: { shown: boolean }) {
  const { t } = useTranslation()
  if (!props.shown) return <span />
  return <p className='text-muted-foreground text-xs'>{t('No requests yet')}</p>
}

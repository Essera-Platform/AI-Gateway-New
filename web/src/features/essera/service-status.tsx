import { useQuery } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { Activity, Wrench } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  dotColorMap,
  textColorMap,
  type StatusVariant,
} from '@/components/status-badge'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { IconBadge } from '@/components/ui/icon-badge'
import { PanelWrapper } from '@/features/dashboard/components/ui/panel-wrapper'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber, formatTimestampRelative } from '@/lib/format'
import { cn } from '@/lib/utils'

import { getEsseraServiceStatus } from './api'
import type {
  EsseraMaintenance,
  EsseraProviderState,
  EsseraProviderStatus,
} from './types'

const SERVICE_STATUS_QUERY_KEY = ['essera', 'status'] as const

const STATE_VARIANT: Record<EsseraProviderState, StatusVariant> = {
  operational: 'success',
  degraded: 'warning',
  down: 'danger',
  maintenance: 'info',
}

function stateLabel(t: TFunction, state: EsseraProviderState): string {
  switch (state) {
    case 'operational':
      return t('Operational')
    case 'degraded':
      return t('Degraded')
    case 'down':
      return t('Down')
    case 'maintenance':
      return t('Maintenance')
  }
}

function formatWhen(
  value: string | null,
  locale: string | undefined
): string | null {
  if (!value) return null
  return new Intl.DateTimeFormat(locale, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(new Date(value))
}

function MaintenanceBanner(props: { maintenance: EsseraMaintenance }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const starts = formatWhen(props.maintenance.starts_at, locale)
  const ends = formatWhen(props.maintenance.ends_at, locale)
  const inProgress = props.maintenance.status === 'in_progress'

  return (
    <Alert className='rounded-none border-x-0 border-t-0 px-4 sm:px-5'>
      <Wrench aria-hidden='true' />
      <AlertTitle>
        {inProgress ? t('Maintenance in progress') : t('Maintenance scheduled')}
      </AlertTitle>
      <AlertDescription>
        {props.maintenance.message && <p>{props.maintenance.message}</p>}
        {starts && ends && (
          <p className='tabular-nums'>
            {t('From {{start}} to {{end}}', { start: starts, end: ends })}
          </p>
        )}
      </AlertDescription>
    </Alert>
  )
}

function ProviderRow(props: { provider: EsseraProviderStatus; last: boolean }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const provider = props.provider
  const variant = STATE_VARIANT[provider.status]
  const checked = provider.checked_at
    ? formatTimestampRelative(
        Date.parse(provider.checked_at),
        'milliseconds',
        locale
      )
    : null

  return (
    <li
      className={cn(
        'flex flex-col gap-1 px-4 py-2.5 sm:px-5',
        !props.last && 'border-border/40 border-b'
      )}
    >
      <div className='flex items-center justify-between gap-2'>
        <div className='flex min-w-0 items-center gap-2.5'>
          <span
            aria-hidden='true'
            className={cn(
              'inline-block size-2 shrink-0 rounded-full',
              dotColorMap[variant]
            )}
          />
          <span className='truncate text-sm font-medium'>{provider.name}</span>
          <span className='text-muted-foreground shrink-0 text-xs'>
            {provider.region}
          </span>
        </div>
        <span
          className={cn('shrink-0 text-xs font-medium', textColorMap[variant])}
        >
          {stateLabel(t, provider.status)}
        </span>
      </div>
      <div className='text-muted-foreground flex flex-wrap gap-x-3 gap-y-0.5 pl-4.5 text-xs tabular-nums'>
        <span>
          {t('Availability')}:{' '}
          {provider.availability_last_hour == null
            ? '-'
            : `${formatNumber(provider.availability_last_hour, locale)}%`}
        </span>
        <span>
          {t('Latency')}:{' '}
          {provider.p50_ms == null
            ? '-'
            : `${formatNumber(provider.p50_ms, locale)} ms`}
        </span>
        {checked && <span>{t('Checked {{when}}', { when: checked })}</span>}
      </div>
    </li>
  )
}

/**
 * A managed gateway's Overview panel for the customer's admin: how each provider the customer
 * may use is doing, as the Console sees it, and any maintenance Essera announced. It replaces
 * new-api's Uptime Kuma panel there.
 */
export function ServiceStatusPanel() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: SERVICE_STATUS_QUERY_KEY,
    queryFn: getEsseraServiceStatus,
    staleTime: 60_000,
    refetchInterval: 60_000,
    meta: { errorToast: false },
  })
  const status = query.data
  const unavailable = query.isError || status?.available === false
  const providers = status?.available ? status.providers : []
  const maintenance = status?.available ? status.maintenance : null

  let emptyMessage = t('No providers available')
  if (unavailable) emptyMessage = t('Status temporarily unavailable')

  return (
    <PanelWrapper
      title={
        <span className='flex items-center gap-2'>
          <IconBadge tone='success' size='sm'>
            <Activity />
          </IconBadge>
          {t('Service status')}
        </span>
      }
      description={t('Provider health over the last hour')}
      loading={query.isPending}
      empty={unavailable || (providers.length === 0 && !maintenance)}
      emptyMessage={emptyMessage}
      height='h-40'
      contentClassName='p-0'
    >
      {maintenance && <MaintenanceBanner maintenance={maintenance} />}
      <ul aria-label={t('Providers')}>
        {providers.map((provider, index) => (
          <ProviderRow
            key={provider.name}
            provider={provider}
            last={index === providers.length - 1}
          />
        ))}
      </ul>
    </PanelWrapper>
  )
}

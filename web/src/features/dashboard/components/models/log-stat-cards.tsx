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
// Modified by Essera, 2026-11: the analytics design: one card per figure with a tinted icon tile, the last one highlighted in emerald; a card for each unit the Console measured that the token and cache cards do not show; a micro amount keeps six decimals.
import { Shapes } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { IconBadge } from '@/components/ui/icon-badge'
import { Skeleton } from '@/components/ui/skeleton'
import { getUserQuotaDates } from '@/features/dashboard/api'
import { useModelStatCardsConfig } from '@/features/dashboard/hooks/use-dashboard-config'
import {
  buildQueryParams,
  calculateDashboardStats,
  getDefaultDays,
} from '@/features/dashboard/lib'
import type {
  QuotaDataItem,
  DashboardFilters,
} from '@/features/dashboard/types'
import { NumberValue } from '@/features/essera/brand/number-value'
import {
  esseraUnitLabel,
  esseraUnitQuantity,
  measuredUnits,
  TOKEN_FIGURE_UNITS,
} from '@/features/essera/model-square-format'
import { amountFractionDigits } from '@/features/essera/money'
import { toIntlLocale } from '@/i18n/languages'
import { formatQuotaWithCurrency } from '@/lib/currency'
import {
  formatCompactNumber,
  formatNumber,
  quotaUnitsToDollars,
} from '@/lib/format'
import { computeTimeRange } from '@/lib/time'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

interface LogStatCardsProps {
  filters?: DashboardFilters
  onDataUpdate?: (data: QuotaDataItem[], loading: boolean) => void
}

const MAX_INLINE_STAT_CHARS = 7

function formatStatNumber(value: number, locale: Intl.LocalesArgument) {
  const fullValue = formatNumber(value, locale)
  const displayValue =
    fullValue.length > MAX_INLINE_STAT_CHARS
      ? formatCompactNumber(value, locale)
      : fullValue

  return {
    displayValue,
    fullValue,
  }
}

function formatAmount(quota: number) {
  const value = formatQuotaWithCurrency(quota, {
    digitsLarge: 2,
    digitsSmall: amountFractionDigits(quotaUnitsToDollars(quota), 4),
    abbreviate: true,
  })
  return { displayValue: value, fullValue: value }
}

export function LogStatCards(props: LogStatCardsProps) {
  const { t, i18n } = useTranslation()
  const statCardsConfig = useModelStatCardsConfig()
  const user = useAuthStore((state) => state.auth.user)
  const isAdmin = !!(user?.role && user.role >= 10)
  const [stats, setStats] = useState<ReturnType<
    typeof calculateDashboardStats
  > | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)

  const [timeRangeMinutes, setTimeRangeMinutes] = useState(0)

  const { filters, onDataUpdate } = props

  useEffect(() => {
    const abortController = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setLoading(true)

    setError(false)
    onDataUpdate?.([], true)

    const timeRange = computeTimeRange(
      getDefaultDays(filters?.time_granularity),
      filters?.start_timestamp,
      filters?.end_timestamp
    )
    const timeDiff = (timeRange.end_timestamp - timeRange.start_timestamp) / 60
    setTimeRangeMinutes(timeDiff)

    void getUserQuotaDates(buildQueryParams(timeRange, filters), isAdmin)
      .then((res) => {
        if (abortController.signal.aborted) return
        const data = res?.data || []
        setStats(calculateDashboardStats(data))
        onDataUpdate?.(data, false)
      })
      .catch(() => {
        if (abortController.signal.aborted) return
        setStats(null)
        setError(true)
        onDataUpdate?.([], false)
      })
      .finally(() => {
        if (!abortController.signal.aborted) {
          setLoading(false)
        }
      })

    return () => {
      abortController.abort()
    }
  }, [filters, isAdmin, onDataUpdate])

  const adaptedStats = {
    rpm: stats?.totalCount ?? 0,
    quota: stats?.totalQuota ?? 0,
    tpm: stats?.totalTokens ?? 0,
    cacheRead: stats?.totalCacheReadTokens ?? 0,
    cacheCreation: stats?.totalCacheCreationTokens ?? 0,
  }

  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const items = statCardsConfig.map((config) => {
    const rawValue = config.getValue(adaptedStats, timeRangeMinutes)
    const formatted =
      config.key === 'quota'
        ? formatAmount(rawValue)
        : formatStatNumber(rawValue, locale)

    return {
      key: config.key,
      title: config.title,
      value: formatted.displayValue,
      fullValue: formatted.fullValue,
      desc: config.description,
      icon: config.icon,
      iconTone: config.iconTone,
    }
  })
  const unitItems = measuredUnits(stats?.totalUnits)
    .filter((measured) => !TOKEN_FIGURE_UNITS.has(measured.unit))
    .map((measured) => {
      const formatted = formatStatNumber(measured.quantity, locale)
      return {
        key: `unit:${measured.unit}`,
        title: esseraUnitLabel(t, measured.unit),
        value: formatted.displayValue,
        fullValue: formatted.fullValue,
        desc: esseraUnitQuantity(t, measured.unit, measured.quantity, locale),
        icon: Shapes,
        iconTone: 'chart-1' as const,
      }
    })
  const averagesAt = items.findIndex((item) => item.key === 'avgRpm')
  items.splice(averagesAt === -1 ? items.length : averagesAt, 0, ...unitItems)

  return (
    <div>
      <div className='grid min-w-0 grid-cols-2 gap-2 sm:grid-cols-3 sm:gap-3 lg:grid-cols-7'>
        {items.map((it, idx) => {
          const Icon = it.icon
          const highlighted = idx === items.length - 1
          let valueContent
          if (loading) {
            valueContent = (
              <div className='mt-1 flex flex-col gap-1 sm:mt-2 sm:gap-1.5'>
                <Skeleton className='h-5 w-16 sm:h-7 sm:w-20' />
                <Skeleton className='hidden h-3.5 w-28 md:block' />
              </div>
            )
          } else if (error) {
            valueContent = (
              <>
                <div className='text-muted-foreground mt-1 font-mono text-base leading-tight font-bold tracking-tight tabular-nums sm:mt-2 sm:text-2xl sm:leading-normal'>
                  --
                </div>
                <div className='text-muted-foreground/40 mt-1 hidden text-xs md:block'>
                  {it.desc}
                </div>
              </>
            )
          } else {
            valueContent = (
              <>
                {/* Modified by Essera, 2026-11: the Essera number style. */}
                <div
                  className='text-foreground mt-1 max-w-full truncate text-base leading-tight font-bold sm:mt-2 sm:text-2xl sm:leading-normal'
                  title={it.fullValue}
                >
                  <NumberValue value={it.value} />
                </div>
                <div className='text-muted-foreground mt-1 hidden truncate text-xs md:block'>
                  {it.desc}
                </div>
              </>
            )
          }

          return (
            <div
              key={it.key}
              role='group'
              aria-label={it.title}
              aria-busy={loading}
              className={cn(
                'bg-card shadow-card min-w-0 rounded-xl border px-3 py-2.5 transition-colors sm:p-3.5',
                highlighted
                  ? 'border-primary/30 bg-primary/5 dark:bg-primary/10'
                  : 'hover:border-primary/30',
                idx === items.length - 1 &&
                  items.length % 2 !== 0 &&
                  'col-span-2 sm:col-span-1'
              )}
            >
              <div className='flex min-w-0 items-center gap-1.5 sm:h-6 sm:gap-2'>
                <IconBadge
                  tone={highlighted ? 'success' : it.iconTone}
                  size='stat'
                  className='size-4 shrink-0 rounded-sm sm:size-5 sm:rounded-md [&>svg]:size-2.5 sm:[&>svg]:size-3'
                >
                  <Icon />
                </IconBadge>
                <div
                  className={cn(
                    'truncate text-[11px] leading-4 font-bold tracking-wider uppercase',
                    highlighted ? 'text-primary' : 'text-muted-foreground'
                  )}
                  title={it.title}
                >
                  {it.title}
                </div>
              </div>

              {valueContent}
            </div>
          )
        })}
      </div>
      {/*<p className='text-muted-foreground border-t px-3 py-2 text-xs'>*/}
      {/*  {t(*/}
      {/*    'Cache usage is recorded only after the upgrade; historical data is not backfilled.'*/}
      {/*  )}*/}
      {/*</p>*/}
    </div>
  )
}

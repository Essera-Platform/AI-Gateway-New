import type { TFunction } from 'i18next'

import { formatCompactNumber } from '@/lib/format'

import { formatBalance, type EsseraAccount } from './account'
import {
  esseraUnitFigure,
  isTokenUnit,
  measuredUnits,
} from './model-square-format'

export type EsseraAccountStat = {
  key: 'spend' | 'limit' | 'requests'
  label: string
  value: string
  description: string
}

/**
 * The account summary of a managed gateway: what the person spent this month and the limit
 * it runs against, as the Console reports them. There is no balance to top up.
 */
export function esseraAccountStats(
  t: TFunction,
  account: EsseraAccount,
  locale?: string
): EsseraAccountStat[] {
  if (!account.available) {
    const unavailable = t('Data is temporarily unavailable')
    return [
      {
        key: 'spend',
        label: t('Spend this month'),
        value: '—',
        description: unavailable,
      },
      {
        key: 'limit',
        label: t('Spend limit'),
        value: '—',
        description: unavailable,
      },
      {
        key: 'requests',
        label: t('API Requests'),
        value: '—',
        description: unavailable,
      },
    ]
  }
  const limit = account.spend_limit
    ? formatBalance(account.spend_limit, account.currency)
    : t('No limit')
  let limitDescription = t('Monthly spend limit')
  if (account.spend_limit && account.spend_left != null) {
    limitDescription = t('{{amount}} left this month', {
      amount: formatBalance(account.spend_left, account.currency),
    })
  }
  return [
    {
      key: 'spend',
      label: t('Spend this month'),
      value: formatBalance(account.spend, account.currency),
      description: [
        t('{{count}} tokens this month', {
          count: formatCompactNumber(account.tokens, locale),
        }),
        ...measuredUnits(account.units)
          .filter((measured) => !isTokenUnit(measured.unit))
          .map((measured) =>
            esseraUnitFigure(t, measured.unit, measured.quantity, locale)
          ),
      ].join(' · '),
    },
    {
      key: 'limit',
      label: t('Spend limit'),
      value: limit,
      description: limitDescription,
    },
    {
      key: 'requests',
      label: t('API Requests'),
      value: formatCompactNumber(account.requests, locale),
      description: t('Requests this month'),
    },
  ]
}

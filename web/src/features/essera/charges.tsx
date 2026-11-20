import { useTranslation } from 'react-i18next'

import {
  DetailRow,
  DetailSection,
} from '@/features/usage-logs/components/dialogs/log-detail-layout'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'

import { formatSpend } from './account'
import { esseraUnitLabel, inBillOrder } from './model-square-format'

/** What one unit of a request came to, as the Console rated it; `amount` null when unpriced. */
export type EsseraCharge = {
  unit: string
  quantity: number
  amount: string | null
  currency: string | null
}

/**
 * A request's bill from the Console: each unit it was charged for (input, cache reads and
 * writes, output, images) with its quantity and amount, then the total.
 */
export function EsseraCharges(props: {
  charges: EsseraCharge[]
  total: string | null | undefined
  currency: string | undefined
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  return (
    <DetailSection label={t('Charges')}>
      {inBillOrder(props.charges).map((charge) => (
        <DetailRow
          key={charge.unit}
          label={esseraUnitLabel(t, charge.unit)}
          value={`${formatNumber(charge.quantity, locale)} · ${
            charge.amount === null
              ? t('Not priced yet')
              : formatSpend(charge.amount, charge.currency ?? '')
          }`}
          mono
        />
      ))}
      <DetailRow
        label={t('Total Cost')}
        value={formatSpend(props.total ?? '-', props.currency ?? '')}
        mono
      />
    </DetailSection>
  )
}

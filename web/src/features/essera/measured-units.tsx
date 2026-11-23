import { useTranslation } from 'react-i18next'

import {
  DetailRow,
  DetailSection,
} from '@/features/usage-logs/components/dialogs/log-detail-layout'
import { toIntlLocale } from '@/i18n/languages'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  esseraUnitFigure,
  esseraUnitLabel,
  measuredUnits,
  TOKEN_FIGURE_UNITS,
  type EsseraUnits,
} from './model-square-format'

/**
 * "Image: 2 · Search unit: 3": what a request measured besides the input, output and cache
 * tokens a log row already shows. Nothing when there is nothing else.
 */
export function EsseraOtherUnits(props: {
  units: EsseraUnits | null | undefined
  className?: string
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const others = measuredUnits(props.units).filter(
    (measured) => !TOKEN_FIGURE_UNITS.has(measured.unit)
  )
  if (others.length === 0) return null
  return (
    <span className={cn('whitespace-nowrap tabular-nums', props.className)}>
      {others
        .map((measured) =>
          esseraUnitFigure(t, measured.unit, measured.quantity, locale)
        )
        .join(' · ')}
    </span>
  )
}

/** Every unit a request measured, as the Console counted it, in the order a bill lists them. */
export function EsseraMeasuredUnits(props: {
  units: EsseraUnits | null | undefined
}) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const measured = measuredUnits(props.units)
  if (measured.length === 0) return null
  return (
    <DetailSection label={t('Usage')}>
      {measured.map((item) => (
        <DetailRow
          key={item.unit}
          label={esseraUnitLabel(t, item.unit)}
          value={formatNumber(item.quantity, locale)}
          mono
        />
      ))}
    </DetailSection>
  )
}

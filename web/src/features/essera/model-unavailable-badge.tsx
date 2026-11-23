import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'

/**
 * The Gateway's recent calls of the model all failed because AWS could not serve it. The
 * model stays listed and callable: AWS may have recovered.
 */
export function EsseraUnavailableBadge(props: { className?: string }) {
  const { t } = useTranslation()
  return (
    <StatusBadge
      variant='warning'
      size='sm'
      label={t('Temporarily unavailable at AWS')}
      copyable={false}
      title={t(
        'Recent requests for this model failed because AWS could not process them. You can still try it.'
      )}
      className={props.className}
    />
  )
}

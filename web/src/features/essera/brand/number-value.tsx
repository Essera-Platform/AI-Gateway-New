import { cn } from '@/lib/utils'

import { splitFigure } from './figure'

/**
 * A figure in the Essera number style: JetBrains Mono, tabular, tight, with its unit in the
 * emerald accent.
 */
export function NumberValue(props: {
  value: string | number
  className?: string
  unitClassName?: string
}) {
  const { figure, unit } = splitFigure(String(props.value))
  return (
    <span className={cn('num', props.className)}>
      {figure}
      {unit && (
        <span className={cn('text-unit', props.unitClassName)}>{unit}</span>
      )}
    </span>
  )
}

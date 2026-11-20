import { Activity, Gauge } from 'lucide-react'

import type { EsseraAccountStat } from './account-summary'
import { NumberValue } from './brand/number-value'

/**
 * The side panel of the dashboard summary on a managed gateway: the spend limit and this
 * month's spend and requests, where new-api shows the balance and the wallet.
 */
export function EsseraSummaryPanel(props: { stats: EsseraAccountStat[] }) {
  const byKey = new Map(props.stats.map((stat) => [stat.key, stat]))
  const limit = byKey.get('limit')
  const spend = byKey.get('spend')
  const requests = byKey.get('requests')

  return (
    <div className='flex flex-col justify-between gap-3 border-t bg-[linear-gradient(135deg,color-mix(in_oklch,var(--overview-accent-2)_12%,var(--background))_0%,color-mix(in_oklch,oklch(0.82_0.04_155)_8%,var(--background))_48%,color-mix(in_oklch,var(--overview-accent-1)_7%,var(--background))_100%)] p-3 sm:gap-4 sm:p-5 xl:border-t-0 xl:border-l'>
      <div className='flex flex-col gap-2 sm:gap-3'>
        <span className='text-muted-foreground text-xs font-medium'>
          {limit?.label}
        </span>
        <div className='text-xl font-extrabold sm:text-3xl'>
          {limit?.value && <NumberValue value={limit.value} />}
        </div>
        <span className='text-muted-foreground text-[11px]'>
          {limit?.description}
        </span>
        <div className='grid grid-cols-2 gap-2'>
          {[
            { stat: spend, icon: Gauge },
            { stat: requests, icon: Activity },
          ].map((cell) =>
            cell.stat ? (
              <div
                key={cell.stat.key}
                className='bg-background/60 rounded-lg px-2.5 py-2'
              >
                <div className='text-muted-foreground flex items-center gap-1 text-[11px] leading-none font-medium'>
                  <cell.icon className='size-3 shrink-0' aria-hidden='true' />
                  <span className='truncate'>{cell.stat.label}</span>
                </div>
                <div className='text-foreground num mt-1.5 truncate text-xs font-semibold'>
                  {cell.stat.value}
                </div>
              </div>
            ) : null
          )}
        </div>
      </div>
    </div>
  )
}

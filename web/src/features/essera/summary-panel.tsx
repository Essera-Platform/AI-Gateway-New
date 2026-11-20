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
    <div className='bg-card flex flex-col justify-between gap-3 border-t p-3 sm:gap-4 sm:p-6 xl:my-6 xl:border-t-0 xl:border-l xl:py-0'>
      <div className='flex flex-col'>
        <span className='text-muted-foreground text-xs font-semibold'>
          {limit?.label}
        </span>
        <div className='mt-2 text-xl font-extrabold tracking-tight sm:text-3xl'>
          {limit?.value && <NumberValue value={limit.value} />}
        </div>
        <span className='text-muted-foreground/80 mt-1 text-[11px]'>
          {limit?.description}
        </span>
      </div>
      <div className='bg-muted/50 flex items-center justify-between gap-3 rounded-xl border p-3 text-xs'>
        {[
          { stat: spend, icon: Gauge, iconClass: 'text-chart-1', end: false },
          {
            stat: requests,
            icon: Activity,
            iconClass: 'text-chart-3',
            end: true,
          },
        ].map((cell) =>
          cell.stat ? (
            <div
              key={cell.stat.key}
              className={cell.end ? 'min-w-0 text-right' : 'min-w-0'}
            >
              <div
                className={
                  cell.end
                    ? 'text-muted-foreground flex items-center justify-end gap-1 text-[11px]'
                    : 'text-muted-foreground flex items-center gap-1.5 text-[11px]'
                }
              >
                <cell.icon
                  className={`size-3 shrink-0 ${cell.iconClass}`}
                  aria-hidden='true'
                />
                <span className='truncate'>{cell.stat.label}</span>
              </div>
              <div className='text-foreground num mt-0.5 truncate text-xs font-bold'>
                {cell.stat.value}
              </div>
            </div>
          ) : null
        )}
      </div>
    </div>
  )
}

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
      <div className='flex flex-col gap-2 sm:gap-3'>
        <span className='text-muted-foreground text-[13px] font-medium'>
          {limit?.label}
        </span>
        <div className='text-xl font-extrabold sm:text-3xl'>
          {limit?.value && <NumberValue value={limit.value} />}
        </div>
        <span className='text-muted-foreground text-xs'>
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
                className='bg-muted/50 rounded-lg border px-3 py-2.5'
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

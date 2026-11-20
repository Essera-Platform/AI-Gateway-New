import { ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { useStatus } from '@/hooks/use-status'

import { NumberValue } from '../brand/number-value'

type Catalog = { models?: number; families?: number }

type Benchmark = {
  key: string
  figure: React.ReactNode
  label: string
  description: string
  footLabel: string
  footValue: string
}

function BenchmarkCard(props: Benchmark) {
  return (
    <div className='group bg-card flex flex-col rounded-2xl border p-6 shadow-[var(--shadow-elevated)] transition-colors hover:border-emerald-300 dark:shadow-none dark:hover:border-emerald-500/40'>
      <div className='text-foreground group-hover:text-primary mb-3 min-h-12 text-4xl font-extrabold transition-colors lg:text-5xl'>
        {props.figure}
      </div>
      <div className='text-foreground mb-1 text-sm font-bold'>
        {props.label}
      </div>
      <p className='text-muted-foreground text-xs leading-relaxed'>
        {props.description}
      </p>
      <div className='mt-auto flex justify-between gap-3 border-t pt-3 font-mono text-[11px] text-emerald-700 uppercase dark:text-emerald-400'>
        <span>{props.footLabel}</span>
        <span className='text-end font-bold'>{props.footValue}</span>
      </div>
    </div>
  )
}

/**
 * The landing's figures, in the design of the benchmarks section: only what the Gateway can
 * back. Models and model families are counted live from the configuration it serves.
 */
export function LandingBenchmarks() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const catalog = (status?.essera_catalog ?? {}) as Catalog
  const count = (value?: number) =>
    typeof value === 'number' ? <NumberValue value={value} /> : '—'

  const cards: Benchmark[] = [
    {
      key: 'models',
      figure: count(catalog.models),
      label: t('Models available'),
      description: t(
        'Active models in the catalog, counted live from the configuration the Gateway serves.'
      ),
      footLabel: t('Source'),
      footValue: t('Live catalog'),
    },
    {
      key: 'families',
      figure: count(catalog.families),
      label: t('Model families'),
      description: t(
        'Distinct model vendors among them, counted the same way.'
      ),
      footLabel: t('Source'),
      footValue: t('Live catalog'),
    },
    {
      key: 'formats',
      figure: <NumberValue value={2} />,
      label: t('API formats'),
      description: t(
        'OpenAI-compatible and Anthropic-compatible, so existing SDKs work unchanged.'
      ),
      footLabel: t('Endpoints'),
      footValue: 'OpenAI · Anthropic',
    },
    {
      key: 'controls',
      figure: (
        <span className='inline-flex items-center gap-3 text-2xl lg:text-3xl'>
          <span className='flex size-11 items-center justify-center rounded-xl bg-emerald-50 text-emerald-600 dark:bg-emerald-500/10 dark:text-emerald-400'>
            <ShieldCheck className='size-6' aria-hidden='true' />
          </span>
          {t('Per account')}
        </span>
      ),
      label: t('Account controls'),
      description: t(
        'Limits on spend, requests, tokens, models and countries, enforced on every request.'
      ),
      footLabel: t('Enforced'),
      footValue: t('Every request'),
    },
  ]

  return (
    <section
      id='benchmarks'
      className='mx-auto max-w-7xl px-4 py-16 sm:px-6 sm:py-20 lg:px-8'
    >
      <div className='mx-auto mb-12 max-w-2xl text-center'>
        <span className='rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1 font-mono text-xs font-bold tracking-widest text-emerald-700 uppercase dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300'>
          {t('At a glance')}
        </span>
        <h2 className='text-foreground mt-4 text-3xl font-extrabold tracking-tight sm:text-4xl'>
          {t('What the Gateway serves today')}
        </h2>
      </div>
      <div className='grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4'>
        {cards.map((card) => (
          <BenchmarkCard {...card} key={card.key} />
        ))}
      </div>
    </section>
  )
}

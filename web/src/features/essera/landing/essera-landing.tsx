import { Link } from '@tanstack/react-router'
import {
  ArrowRight,
  BarChart3,
  ClipboardList,
  KeyRound,
  LayoutGrid,
  Plug,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { Footer } from '@/components/layout/components/footer'
import { cn } from '@/lib/utils'

import { LandingBenchmarks } from './benchmarks'
import { LandingTerminal } from './landing-terminal'

const PRIMARY_CTA =
  'inline-flex items-center justify-center gap-2.5 rounded-xl bg-primary px-6 py-3.5 text-sm font-semibold text-primary-foreground shadow-md shadow-emerald-600/20 transition-all duration-200 hover:-translate-y-0.5 hover:bg-emerald-800 dark:hover:bg-emerald-400'
const SECONDARY_CTA =
  'inline-flex items-center justify-center gap-2 rounded-xl border border-slate-300 bg-card px-6 py-3.5 text-sm font-semibold text-foreground shadow-sm transition-all duration-200 hover:border-slate-400 hover:bg-muted dark:border-slate-700 dark:hover:border-slate-600'

function Hero(props: { isAuthenticated: boolean }) {
  const { t } = useTranslation()
  return (
    <section className='mx-auto max-w-7xl px-4 pt-32 pb-16 sm:px-6 sm:pt-36 lg:px-8 lg:pb-24'>
      <div className='grid grid-cols-1 items-center gap-12 lg:grid-cols-12 lg:gap-8'>
        <div className='flex flex-col items-start text-left lg:col-span-6'>
          <div className='mb-6 inline-flex items-center gap-2 rounded-full border border-emerald-200/80 bg-emerald-50 px-3 py-1.5 text-xs font-semibold tracking-wider text-emerald-700 uppercase shadow-sm dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300'>
            <span
              aria-hidden='true'
              className='size-2 rounded-full bg-emerald-500 motion-safe:animate-pulse'
            />
            Essera AI Gateway
          </div>
          <h1 className='text-foreground mb-6 text-4xl leading-[1.12] font-extrabold tracking-tight sm:text-5xl lg:text-6xl'>
            {t('One gateway for')}
            <br />
            <span className='essera-gradient-text'>
              {t('every model your teams use')}
            </span>
          </h1>
          <p className='text-sidebar-foreground mb-8 max-w-xl text-base leading-relaxed sm:text-lg'>
            {t(
              'Call AWS Bedrock models through one OpenAI- and Anthropic-compatible API, with your organisation’s limits, models and countries enforced on every request.'
            )}
          </p>
          <div className='flex w-full flex-wrap items-center gap-4 sm:w-auto'>
            {props.isAuthenticated ? (
              <Link to='/dashboard' className={PRIMARY_CTA}>
                {t('Go to Dashboard')}
                <ArrowRight className='size-4' aria-hidden='true' />
              </Link>
            ) : (
              <Link to='/sign-in' className={PRIMARY_CTA}>
                {t('Get Started')}
                <ArrowRight className='size-4' aria-hidden='true' />
              </Link>
            )}
            <Link to='/pricing' className={SECONDARY_CTA}>
              <LayoutGrid
                className='text-muted-foreground size-4'
                aria-hidden='true'
              />
              {t('Model Square')}
            </Link>
          </div>
        </div>
        <div className='min-w-0 lg:col-span-6'>
          <LandingTerminal />
        </div>
      </div>
    </section>
  )
}

function HowItWorks() {
  const { t } = useTranslation()
  const steps = [
    {
      icon: KeyRound,
      title: t('Create a key'),
      text: t(
        'Sign in with your work email and create an API key. It is shown once; only you hold it.'
      ),
    },
    {
      icon: Plug,
      title: t('Connect'),
      text: t(
        'Point an OpenAI- or Anthropic-compatible SDK at the Gateway. Your code stays as it is.'
      ),
    },
    {
      icon: BarChart3,
      title: t('Monitor'),
      text: t(
        'Follow requests, tokens and spend in the dashboard and the usage logs.'
      ),
    },
  ]
  return (
    <section className='mx-auto max-w-7xl px-4 py-20 sm:px-6 lg:px-8 lg:py-28'>
      <div className='mx-auto mb-16 max-w-2xl text-center'>
        <span className='rounded-full border border-emerald-200 bg-emerald-50 px-3 py-1 text-xs font-bold tracking-widest text-emerald-700 uppercase dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300'>
          {t('How It Works')}
        </span>
        <h2 className='text-foreground mt-4 text-3xl font-extrabold tracking-tight sm:text-4xl'>
          {t('Three steps to get started')}
        </h2>
        <p className='text-muted-foreground mt-3 text-sm sm:text-base'>
          {t('Start calling models in minutes, without changing your code.')}
        </p>
      </div>
      <ol className='grid grid-cols-1 gap-8 md:grid-cols-3'>
        {steps.map((step, index) => (
          <li
            key={step.title}
            className='group bg-card flex flex-col items-center rounded-2xl border p-8 text-center shadow-sm transition-all hover:border-emerald-200 hover:shadow-md dark:hover:border-emerald-500/30'
          >
            <div className='relative mb-6 flex size-16 items-center justify-center rounded-full border border-emerald-100 bg-emerald-50 dark:border-emerald-500/20 dark:bg-emerald-500/10'>
              <span className='bg-foreground text-background ring-card absolute -top-1.5 -right-1.5 flex size-6 items-center justify-center rounded-full font-mono text-xs font-bold ring-2'>
                {index + 1}
              </span>
              <step.icon
                className='size-7 text-emerald-700 dark:text-emerald-400'
                aria-hidden='true'
              />
            </div>
            <h3 className='text-foreground mb-2.5 text-xl font-bold'>
              {step.title}
            </h3>
            <p className='text-muted-foreground max-w-xs text-sm leading-relaxed'>
              {step.text}
            </p>
          </li>
        ))}
      </ol>
    </section>
  )
}

function ManagedCallout() {
  const { t } = useTranslation()
  const points = [
    {
      icon: KeyRound,
      title: t('Keys shown once'),
      text: t('Create and revoke your own keys'),
    },
    {
      icon: ClipboardList,
      title: t('Audit log'),
      text: t('Key and user changes are recorded'),
    },
  ]
  return (
    <section className='mx-auto mb-20 max-w-7xl px-4 sm:px-6 lg:px-8'>
      <div className='relative overflow-hidden rounded-3xl border border-emerald-800/40 bg-gradient-to-br from-slate-900 via-[#151e2e] to-[#022c22] p-8 text-white shadow-2xl sm:p-12'>
        <div
          aria-hidden='true'
          className='pointer-events-none absolute -right-20 -bottom-20 size-80 rounded-full bg-emerald-500/10 blur-3xl'
        />
        <div className='relative z-10 grid grid-cols-1 items-center gap-8 lg:grid-cols-12'>
          <div className='lg:col-span-8'>
            <span className='mb-4 inline-flex items-center rounded-full border border-emerald-500/30 bg-emerald-500/20 px-3 py-1 text-xs font-semibold tracking-wider text-emerald-300 uppercase'>
              {t('Managed by the Essera Operations Console')}
            </span>
            <h3 className='mb-3 text-2xl font-extrabold tracking-tight text-white sm:text-3xl'>
              {t('Your organisation’s rules on every request')}
            </h3>
            <p className='max-w-2xl text-sm leading-relaxed text-slate-300 sm:text-base'>
              {t(
                'Your administrator sets in the Essera Operations Console which models and countries each account may use and its monthly limits. The Gateway checks them before a request reaches a model.'
              )}
            </p>
          </div>
          <div className='flex flex-col justify-center gap-3 sm:flex-row lg:col-span-4 lg:flex-col'>
            {points.map((point) => (
              <div
                key={point.title}
                className={cn(
                  'flex flex-1 items-center gap-3 rounded-xl border border-white/10 bg-white/5 p-3.5 backdrop-blur-xs'
                )}
              >
                <div className='flex size-9 shrink-0 items-center justify-center rounded-lg bg-emerald-500/20 text-emerald-400'>
                  <point.icon className='size-4' aria-hidden='true' />
                </div>
                <div className='text-xs'>
                  <p className='font-semibold text-white'>{point.title}</p>
                  <p className='text-slate-400'>{point.text}</p>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </section>
  )
}

/** The public home page of a managed gateway: the "Modern Green Fintech" design. */
export function EsseraLanding(props: { isAuthenticated: boolean }) {
  return (
    <PublicLayout showMainContainer={false}>
      <div className='bg-marketing relative'>
        <div
          aria-hidden='true'
          className='essera-grid-bg pointer-events-none fixed inset-0 opacity-70'
        />
        <main className='relative'>
          <Hero isAuthenticated={props.isAuthenticated} />
          <div className='border-y bg-white/70 backdrop-blur-sm dark:bg-white/[0.02]'>
            <LandingBenchmarks />
          </div>
          <HowItWorks />
          <ManagedCallout />
        </main>
        <Footer className='bg-card relative' />
      </div>
    </PublicLayout>
  )
}

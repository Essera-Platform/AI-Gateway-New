import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  cleanup,
  render,
  renderHook,
  screen,
  waitFor,
} from '@testing-library/react'
import type { TFunction } from 'i18next'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { checkedAgo } from '@/features/essera/model-square-format'
import { ModelCard } from '@/features/pricing/components/model-card'
import { ModelDetailsContent } from '@/features/pricing/components/model-details'
import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import type { PricingModel } from '@/features/pricing/types'
import { api } from '@/lib/api'

vi.mock('@visactor/react-vchart', () => ({ VChart: () => null }))

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

const claude = {
  model_name: 'anthropic.claude-sonnet-4',
  display_name: 'Claude Sonnet 4',
  model_type: 'text',
  vendor_id: 0,
  quota_type: 0,
  model_ratio: 0,
  model_price: 0,
  completion_ratio: 0,
  enable_groups: [],
  supported_endpoint_types: [],
}

async function managedModel(
  signedIn: boolean,
  item: object,
  pricesAvailable = true
) {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/status') {
      return { data: { success: true, data: { essera_managed: true } } }
    }
    return {
      data: {
        success: true,
        data: [item],
        vendors: signedIn ? [{ id: 1, name: 'AWS Bedrock' }] : [],
        group_ratio: {},
        usable_group: {},
        auto_groups: [],
        supported_endpoint: {},
        essera: { signed_in: signedIn, prices_available: pricesAvailable },
      },
    }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  function Wrapper(props: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        {props.children}
      </QueryClientProvider>
    )
  }
  const { result } = renderHook(() => usePricingData(), { wrapper: Wrapper })
  await waitFor(() => expect(result.current.models).toHaveLength(1))
  return result.current.models[0]
}

describe('Model Square on a managed gateway', () => {
  it('shows a guest the display name and type but no price or details', async () => {
    const model = await managedModel(false, claude)

    render(<ModelCard model={model} onClick={vi.fn()} />)

    expect(screen.getByText('Claude Sonnet 4')).toBeVisible()
    expect(screen.getByText('Text')).toBeVisible()
    expect(screen.getByText('Sign in to see prices')).toBeVisible()
    expect(screen.queryByText(/USD/)).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Details' })
    ).not.toBeInTheDocument()
  })

  it('shows a signed-in person their own prices per 1M tokens and per image', async () => {
    const model = await managedModel(true, {
      ...claude,
      vendor_id: 1,
      essera_prices: [
        {
          unit: 'input_tokens',
          amount: '2.70',
          per_quantity: 1_000_000,
          currency: 'USD',
        },
        {
          unit: 'output_tokens',
          amount: '13.50',
          per_quantity: 1_000_000,
          currency: 'USD',
        },
        { unit: 'images', amount: '0.04', per_quantity: 1, currency: 'USD' },
        {
          unit: 'requests',
          amount: '0.10',
          per_quantity: 1000,
          currency: 'USD',
        },
      ],
    })

    render(<ModelCard model={model} onClick={vi.fn()} />)

    expect(screen.getByText('Input').parentElement).toHaveTextContent(
      '2.70 USD / 1M tokens'
    )
    expect(screen.getByText('Output').parentElement).toHaveTextContent(
      '13.50 USD / 1M tokens'
    )
    expect(screen.getByText('Image').parentElement).toHaveTextContent(
      '0.04 USD / image'
    )
    expect(screen.getByText('Request').parentElement).toHaveTextContent(
      '0.10 USD / 1000 requests'
    )
    expect(screen.getByText('AWS Bedrock')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Details' })).toBeVisible()
    expect(screen.queryByText('Sign in to see prices')).not.toBeInTheDocument()
  })

  it('says when the Console cannot give a signed-in person their prices', async () => {
    const model = await managedModel(true, { ...claude, vendor_id: 1 }, false)

    render(<ModelCard model={model} onClick={vi.fn()} />)

    expect(screen.getByText('Prices are temporarily unavailable')).toBeVisible()
    expect(screen.queryByText('No price available')).not.toBeInTheDocument()
  })

  it('shows no groups and no figures for a model the Console has none for', async () => {
    const model = await managedModel(true, {
      ...claude,
      vendor_id: 1,
      enable_groups: ['essera'],
    })

    render(
      <ModelCard
        model={model}
        onClick={vi.fn()}
        perf={{ avg_latency_ms: 850, success_rate: 98.75, avg_tps: 600 }}
      />
    )

    expect(screen.queryByText('Availability')).not.toBeInTheDocument()
    expect(screen.queryByText('Status')).not.toBeInTheDocument()
    expect(screen.queryByText('850ms')).not.toBeInTheDocument()
    expect(screen.queryByText('Groups')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Details' })).toBeVisible()
  })

  it("shows the Console's availability, first-token time and speed, and when it was checked", async () => {
    const checkedAt = new Date(Date.now() - 5 * 60_000 - 1_000).toISOString()
    const model = await managedModel(true, {
      ...claude,
      vendor_id: 1,
      essera_metrics: {
        provider: 'bedrock',
        model: claude.model_name,
        availability_24h: 99.5,
        p50_ttft_ms: 420,
        p95_ttft_ms: 900,
        p50_latency_ms: 1100,
        output_per_second: 85.2,
        traffic_requests_24h: 40,
        probe_requests_24h: 48,
        checked_at: checkedAt,
      },
    })

    render(<ModelCard model={model} onClick={vi.fn()} />)

    expect(screen.getByText('Availability').nextSibling).toHaveTextContent(
      '99.50%'
    )
    expect(screen.getByText('TTFT').nextSibling).toHaveTextContent('420ms')
    expect(screen.getByText('85.2 t/s')).toBeVisible()
    expect(screen.getByText('Checked 5 min ago')).toBeVisible()
  })

  it('shows a figure the Console has no data for as a dash, never as zero', async () => {
    const model = await managedModel(true, {
      ...claude,
      vendor_id: 1,
      model_type: 'embedding',
      essera_metrics: {
        provider: 'bedrock',
        model: claude.model_name,
        availability_24h: 100,
        p50_ttft_ms: null,
        p95_ttft_ms: null,
        p50_latency_ms: 300,
        output_per_second: null,
        traffic_requests_24h: 0,
        probe_requests_24h: 48,
        checked_at: null,
      },
    })

    render(<ModelCard model={model} onClick={vi.fn()} />)

    expect(screen.getByText('TTFT').nextSibling).toHaveTextContent('—')
    expect(screen.queryByText(/0ms|0\.00 t\/s/)).not.toBeInTheDocument()
    expect(screen.queryByText(/Checked/)).not.toBeInTheDocument()
  })

  it('shows a guest no figures', async () => {
    const model = await managedModel(false, claude)

    render(<ModelCard model={model} onClick={vi.fn()} />)

    expect(screen.queryByText('Availability')).not.toBeInTheDocument()
    expect(screen.queryByText(/t\/s/)).not.toBeInTheDocument()
  })
})

describe('Model details on a managed gateway', () => {
  function details(model: PricingModel) {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <ModelDetailsContent
          model={model}
          groupRatio={{}}
          usableGroup={{}}
          endpointMap={{}}
          autoGroups={[]}
          priceRate={1}
          usdExchangeRate={1}
          tokenUnit='M'
        />
      </QueryClientProvider>
    )
  }

  it("leads with the Console's figures and has no tab of the Gateway's own counts", async () => {
    const model = await managedModel(true, {
      ...claude,
      vendor_id: 1,
      essera_metrics: {
        provider: 'bedrock',
        model: claude.model_name,
        availability_24h: 97.25,
        p50_ttft_ms: 1250,
        p95_ttft_ms: 3000,
        p50_latency_ms: 2000,
        output_per_second: 40,
        traffic_requests_24h: 3,
        probe_requests_24h: 48,
        checked_at: new Date().toISOString(),
      },
    })
    const get = vi.spyOn(api, 'get')
    details(model)

    expect(screen.getByText('Availability').nextSibling).toHaveTextContent(
      '97.25%'
    )
    expect(screen.getByText('TTFT').nextSibling).toHaveTextContent('1.25s')
    expect(screen.getByText('Checked just now')).toBeVisible()
    expect(
      screen.queryByRole('tab', { name: /Performance/ })
    ).not.toBeInTheDocument()
    const perfCalls = get.mock.calls.filter(([url]) =>
      String(url).includes('perf')
    )
    expect(perfCalls).toHaveLength(0)
  })

  it('shows no figures while the Console has none', async () => {
    const model = await managedModel(true, { ...claude, vendor_id: 1 })
    details(model)

    expect(screen.queryByText('Availability')).not.toBeInTheDocument()
    expect(screen.queryByText('Success rate')).not.toBeInTheDocument()
  })
})

describe('checkedAgo', () => {
  const t = ((key: string, values?: Record<string, number>) =>
    key.replaceAll(/{{(\w+)}}/g, (_, name: string) =>
      String(values?.[name])
    )) as unknown as TFunction
  const now = Date.parse('2026-11-20T12:00:00Z')

  it('says how long ago the newest data point was', () => {
    expect(checkedAgo(t, '2026-11-20T11:59:30Z', now)).toBe('Checked just now')
    expect(checkedAgo(t, '2026-11-20T11:31:00Z', now)).toBe(
      'Checked 29 min ago'
    )
    expect(checkedAgo(t, '2026-11-20T09:00:00Z', now)).toBe('Checked 3 h ago')
    expect(checkedAgo(t, null, now)).toBeNull()
    expect(checkedAgo(t, 'not a date', now)).toBeNull()
  })
})

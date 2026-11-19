import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  cleanup,
  render,
  renderHook,
  screen,
  waitFor,
} from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ModelCard } from '@/features/pricing/components/model-card'
import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'
import { api } from '@/lib/api'

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
    expect(screen.getByText('text')).toBeVisible()
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

  it('shows no groups and one quiet line for a model nobody has called', async () => {
    const model = await managedModel(true, {
      ...claude,
      vendor_id: 1,
      enable_groups: ['essera'],
    })

    render(<ModelCard model={model} onClick={vi.fn()} />)

    expect(screen.getByText('No requests yet')).toBeVisible()
    expect(screen.queryByText('Status')).not.toBeInTheDocument()
    expect(screen.queryByText('Groups')).not.toBeInTheDocument()
  })

  it('keeps the performance figures of a model with traffic', async () => {
    const model = await managedModel(true, { ...claude, vendor_id: 1 })

    render(
      <ModelCard
        model={model}
        onClick={vi.fn()}
        perf={{ avg_latency_ms: 850, success_rate: 98.75, avg_tps: 600 }}
      />
    )

    expect(screen.getByText('Status')).toBeVisible()
    expect(screen.getByText('850ms')).toBeVisible()
    expect(screen.queryByText('No requests yet')).not.toBeInTheDocument()
  })
})

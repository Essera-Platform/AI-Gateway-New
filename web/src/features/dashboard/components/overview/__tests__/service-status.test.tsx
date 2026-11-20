// Essera: on a managed gateway the Overview shows the customer's admin the Console's service
// status in place of Uptime Kuma; a user does not see it.
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ESSERA_ACCOUNT_QUERY_KEY } from '@/features/essera/account'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'

import { OverviewDashboard } from '../overview-dashboard'

const STATUS = {
  available: true,
  providers: [
    {
      name: 'AWS Bedrock',
      region: 'eu-west-1',
      status: 'degraded',
      availability_last_hour: 93.5,
      p50_ms: 840,
      checked_at: new Date().toISOString(),
      models: 12,
    },
  ],
  maintenance: {
    status: 'scheduled',
    message: 'Gateway upgrade',
    starts_at: '2026-11-22T02:00:00Z',
    ends_at: '2026-11-22T03:00:00Z',
  },
}

let client: QueryClient
let esseraRole: 'admin' | 'user'
let statusAnswer: () => unknown
let statusAsked: number

beforeEach(() => {
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
  useAuthStore.getState().auth.setUser({
    id: 7,
    username: 'owner@helios.example',
    role: 1,
    quota: 0,
    used_quota: 0,
    request_count: 1,
  })
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  esseraRole = 'admin'
  statusAnswer = () => ({ data: { success: true, data: STATUS } })
  statusAsked = 0
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    switch (url) {
      case '/api/status':
        return {
          data: {
            data: {
              essera_managed: true,
              api_info_enabled: false,
              announcements_enabled: false,
              faq_enabled: false,
              uptime_kuma_enabled: false,
            },
          },
        }
      case '/api/user/self':
        return {
          data: {
            success: true,
            data: {
              id: 7,
              role: 1,
              essera: { role: esseraRole, available: true },
            },
          },
        }
      case '/api/essera/status':
        statusAsked += 1
        return statusAnswer()
      case '/api/token/?p=1&size=10':
        return { data: { success: true, data: { items: [] } } }
      case '/api/user/models':
        return { data: { success: true, data: ['amazon.nova-lite-v1:0'] } }
      case '/api/data/self':
        return { data: { success: true, data: [] } }
      default:
        throw new Error(`Unexpected dashboard request: ${url}`)
    }
  })
})

afterEach(() => {
  cleanup()
  client.clear()
  vi.restoreAllMocks()
  useAuthStore.setState(useAuthStore.getInitialState(), true)
  useSystemConfigStore.setState(useSystemConfigStore.getInitialState(), true)
})

async function renderOverview() {
  const router = createRouter({
    routeTree: createRootRoute({ component: OverviewDashboard }),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  return render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('managed overview service status', () => {
  it('shows the admin each provider with its state, numbers and the announced maintenance', async () => {
    await renderOverview()

    const providers = await screen.findByRole('list', { name: 'Providers' })
    const row = within(providers).getByRole('listitem')
    expect(row).toHaveTextContent('AWS Bedrock')
    expect(row).toHaveTextContent('eu-west-1')
    expect(row).toHaveTextContent('Degraded')
    expect(row).toHaveTextContent('93.5%')
    expect(row).toHaveTextContent('840 ms')
    expect(row).toHaveTextContent(/Checked .*now|Checked .*second/)
    expect(screen.getByText('Service status')).toBeInTheDocument()
    const banner = screen.getByRole('alert')
    expect(banner).toHaveTextContent('Maintenance scheduled')
    expect(banner).toHaveTextContent('Gateway upgrade')
    expect(screen.queryByText('Uptime')).not.toBeInTheDocument()
  })

  it('does not show a user the panel or ask for the status', async () => {
    esseraRole = 'user'
    await renderOverview()

    await waitFor(() =>
      expect(client.getQueryState(ESSERA_ACCOUNT_QUERY_KEY)?.status).toBe(
        'success'
      )
    )
    await screen.findByRole('heading', { name: 'Usage at a glance' })
    expect(screen.queryByText('Service status')).not.toBeInTheDocument()
    expect(statusAsked).toBe(0)
  })

  it('says the status is temporarily unavailable when the Console does not answer', async () => {
    statusAnswer = () => ({
      data: { success: true, data: { available: false } },
    })
    await renderOverview()

    expect(
      await screen.findByText('Status temporarily unavailable')
    ).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Providers' })).toBeNull()
  })

  it('says the status is temporarily unavailable when the request fails', async () => {
    statusAnswer = () => {
      throw new Error('Network Error')
    }
    await renderOverview()

    expect(
      await screen.findByText('Status temporarily unavailable')
    ).toBeInTheDocument()
  })
})

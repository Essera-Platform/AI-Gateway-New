import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
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
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { Toaster, toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { apiKeySchema } from '../../types'
import { ApiKeysDialogs } from '../api-keys-dialogs'
import { ApiKeysPrimaryButtons } from '../api-keys-primary-buttons'
import { ApiKeysProvider } from '../api-keys-provider'
import { ApiKeysTable } from '../api-keys-table'

const NINETY_DAYS_S = 90 * 24 * 60 * 60
const createdKey = `sk-0123456789abcdef${'S'.repeat(40)}`
const listed = apiKeySchema.parse({
  id: 7,
  name: 'production',
  key: '0123**********SSSS',
  status: 1,
  remain_quota: 0,
  used_quota: 0,
  unlimited_quota: true,
  expired_time: 1_900_000_000,
  created_time: 0,
  accessed_time: 0,
  group: 'essera',
  model_limits_enabled: false,
})

function KeysPage() {
  return (
    <ApiKeysProvider>
      <ApiKeysPrimaryButtons />
      <ApiKeysTable />
      <ApiKeysDialogs />
      <Toaster />
    </ApiKeysProvider>
  )
}

async function renderKeysPage(managed: boolean) {
  const status = managed ? { essera_managed: true } : {}
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/status') return { data: { success: true, data: status } }
    if (url.startsWith('/api/token/')) {
      return { data: { success: true, data: { items: [listed], total: 1 } } }
    }
    return { data: { success: true, data: { default: { ratio: 1 } } } }
  })
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      data: {
        id: 8,
        key_id: 'k_0123456789abcdef',
        key: createdKey,
        name: 'Production',
        expires_at: '2027-02-17T00:00:00Z',
      },
    },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], status)
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const keysRoute = createRoute({
    getParentRoute: () => auth,
    path: 'keys/',
    component: KeysPage,
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([keysRoute])]),
    history: createMemoryHistory({ initialEntries: ['/keys/'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
  await screen.findByText(listed.name)
  return { post }
}

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  localStorage.clear()
})

afterEach(() => {
  cleanup()
  toast.dismiss()
  vi.restoreAllMocks()
  localStorage.clear()
})

describe('API keys on a managed gateway', () => {
  it('lists keys without the columns and actions the Console owns', async () => {
    await renderKeysPage(true)

    for (const name of ['Group', 'Models', 'IP Restriction']) {
      expect(
        screen.queryByRole('columnheader', { name })
      ).not.toBeInTheDocument()
    }
    expect(
      screen.queryByRole('columnheader', { name: /Quota/ })
    ).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Delete' })).toBeInTheDocument()
    for (const name of ['Edit', 'Disable', 'Open menu', 'Copy API key']) {
      expect(screen.queryByRole('button', { name })).not.toBeInTheDocument()
    }
  })

  it('creates a key with a name and a 90-day expiry and shows it once', async () => {
    const { post } = await renderKeysPage(true)
    const user = userEvent.setup()
    const before = Math.floor(Date.now() / 1000)

    await user.click(screen.getByRole('button', { name: 'Create API Key' }))
    const drawer = await screen.findByRole('dialog')
    expect(within(drawer).queryByText('Group')).not.toBeInTheDocument()
    expect(within(drawer).queryByText('Quota Settings')).not.toBeInTheDocument()
    expect(
      within(drawer).queryByText('Advanced Settings')
    ).not.toBeInTheDocument()
    await user.type(within(drawer).getByLabelText('Name'), 'Production')
    await user.click(within(drawer).getByRole('button', { name: 'Create' }))

    const shown = await screen.findByText(createdKey)
    expect(
      screen.getByText('Copy this key now. It will not be shown again.')
    ).toBeInTheDocument()
    const [url, payload] = post.mock.calls[0] as [
      string,
      { name: string; expired_time: number },
    ]
    expect(url).toBe('/api/token/')
    expect(payload.name).toBe('Production')
    expect(payload.expired_time - before).toBeGreaterThanOrEqual(
      NINETY_DAYS_S - 5
    )
    expect(payload.expired_time - before).toBeLessThanOrEqual(NINETY_DAYS_S + 5)

    await user.click(screen.getByRole('button', { name: 'Done' }))
    await waitFor(() => expect(shown).not.toBeInTheDocument())
    expect(screen.queryByText(createdKey)).not.toBeInTheDocument()
  })
  it('shows the gateway refusal in the reader language and no key', async () => {
    const refusal = 'Keys are created by the people of a Console customer.'
    i18next.addResource('en', 'translation', refusal, 'Only customer people.')
    const { post } = await renderKeysPage(true)
    post.mockResolvedValueOnce({ data: { success: false, message: refusal } })
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Create API Key' }))
    const drawer = await screen.findByRole('dialog')
    await user.type(within(drawer).getByLabelText('Name'), 'Mine')
    await user.click(within(drawer).getByRole('button', { name: 'Create' }))

    expect(await screen.findByText('Only customer people.')).toBeInTheDocument()
    expect(screen.queryByText(createdKey)).not.toBeInTheDocument()
    i18next.addResource('en', 'translation', refusal, refusal)
  })
})

describe('API keys on an unmanaged gateway', () => {
  it('keeps the upstream columns and row actions', async () => {
    await renderKeysPage(false)

    for (const name of ['Group', 'Models', 'IP Restriction']) {
      expect(screen.getByRole('columnheader', { name })).toBeInTheDocument()
    }
    expect(screen.getByRole('button', { name: 'Edit' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Disable' })).toBeInTheDocument()
  })
})

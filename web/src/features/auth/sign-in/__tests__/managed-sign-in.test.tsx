import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SignIn } from '..'

let search: Record<string, unknown> = {}

vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  useSearch: () => search,
}))

function renderSignIn(status: Record<string, unknown>) {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/status') return { data: { success: true, data: status } }
    return { data: { success: true, data: {} } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], status)
  const root = createRootRoute({ component: Outlet })
  const router = createRouter({
    routeTree: root.addChildren([
      createRoute({
        getParentRoute: () => root,
        path: '/sign-in',
        component: SignIn,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: ['/sign-in'] }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

beforeEach(() => {
  search = {}
  localStorage.clear()
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  localStorage.clear()
})

describe('Sign-in page', () => {
  it('on a managed gateway offers the Essera sign-in and no registration', async () => {
    renderSignIn({ essera_managed: true, essera_sign_in: 'password' })

    expect(await screen.findByLabelText('Email')).toBeInTheDocument()
    expect(screen.queryByLabelText('Username or Email')).not.toBeInTheDocument()
    expect(
      screen.queryByRole('link', { name: 'Sign up' })
    ).not.toBeInTheDocument()
    expect(screen.queryByText(/operator/i)).not.toBeInTheDocument()
  })

  it('gives operators their sign-in only at its own address', async () => {
    search = { operator: true }
    renderSignIn({ essera_managed: true, essera_sign_in: 'email_otp' })

    expect(
      await screen.findByLabelText('Username or Email')
    ).toBeInTheDocument()
    expect(screen.queryByLabelText('Email')).not.toBeInTheDocument()
  })

  it('keeps the upstream sign-in unchanged on an unmanaged gateway', async () => {
    renderSignIn({ register_enabled: true })

    expect(
      await screen.findByLabelText('Username or Email')
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Sign up' })).toBeInTheDocument()
  })
})

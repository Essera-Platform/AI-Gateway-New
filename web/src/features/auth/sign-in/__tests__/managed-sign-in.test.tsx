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
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { SignIn } from '..'

vi.mock('@tanstack/react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@tanstack/react-router')>()),
  useSearch: () => ({}),
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
  })

  it('switches to the operator sign-in and back on a managed gateway', async () => {
    renderSignIn({ essera_managed: true, essera_sign_in: 'password' })
    const user = userEvent.setup()

    await user.click(
      await screen.findByRole('button', { name: 'Operator sign-in' })
    )
    expect(
      await screen.findByLabelText('Username or Email')
    ).toBeInTheDocument()
    expect(screen.queryByLabelText('Email')).not.toBeInTheDocument()

    await user.click(
      screen.getByRole('button', { name: 'Back to Essera sign-in' })
    )
    expect(await screen.findByLabelText('Email')).toBeInTheDocument()
  })

  it('keeps the upstream sign-in unchanged on an unmanaged gateway', async () => {
    renderSignIn({ register_enabled: true })

    expect(
      await screen.findByLabelText('Username or Email')
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Sign up' })).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Operator sign-in' })
    ).not.toBeInTheDocument()
  })
})

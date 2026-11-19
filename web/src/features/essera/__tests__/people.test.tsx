import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  isRedirect,
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
import { Toaster, toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { Route as UsersRoute } from '@/routes/_authenticated/users/index'
import { useAuthStore } from '@/stores/auth-store'

import { EsseraPeople } from '../people'

const owner = {
  id: 'owner',
  email: 'owner@acme.example',
  role: 'admin',
  status: 'active',
  key_count: 1,
  self: true,
}
const endUser = {
  id: 'user-1',
  email: 'user-1@acme.example',
  role: 'user',
  status: 'active',
  key_count: 2,
}

type Answer = { status?: number; body: object }

function peoplePage(list: Answer) {
  const get = vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/status') {
      return { data: { success: true, data: { essera_managed: true } } }
    }
    return { status: list.status ?? 200, data: list.body }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const root = createRootRoute()
  const auth = createRoute({ getParentRoute: () => root, id: '_authenticated' })
  const users = createRoute({
    getParentRoute: () => auth,
    path: 'users/',
    component: () => (
      <>
        <EsseraPeople />
        <Toaster />
      </>
    ),
  })
  const router = createRouter({
    routeTree: root.addChildren([auth.addChildren([users])]),
    history: createMemoryHistory({ initialEntries: ['/users/'] }),
  })
  return {
    get,
    async render() {
      await router.load()
      render(
        <QueryClientProvider client={client}>
          <RouterProvider router={router} />
        </QueryClientProvider>
      )
    },
  }
}

const listed = {
  body: {
    success: true,
    data: { page: 1, page_size: 20, total: 2, items: [owner, endUser] },
  },
}

function row(email: string) {
  return screen.getByText(email).closest('tr') as HTMLElement
}

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  localStorage.clear()
})

afterEach(() => {
  cleanup()
  toast.dismiss()
  vi.restoreAllMocks()
  useAuthStore.getState().auth.reset()
})

describe("An admin's users page", () => {
  it('offers Disable and Delete on end users but nothing on the owner or the admin', async () => {
    const page = peoplePage(listed)
    await page.render()
    await screen.findByText(endUser.email)
    const user = userEvent.setup()

    expect(
      within(row(owner.email)).queryByRole('button', { name: 'Open menu' })
    ).not.toBeInTheDocument()
    await user.click(
      within(row(endUser.email)).getByRole('button', { name: 'Open menu' })
    )
    expect(
      await screen.findByRole('menuitem', { name: /Disable/ })
    ).toBeVisible()
    expect(screen.getByRole('menuitem', { name: /Delete/ })).toBeVisible()
    expect(
      screen.queryByRole('menuitem', { name: /Promote|Edit/ })
    ).not.toBeInTheDocument()
  })

  it('suspends at the Console and shows the expected state as pending', async () => {
    const page = peoplePage(listed)
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      status: 200,
      data: {
        success: true,
        data: { ...endUser, status: 'suspended', pending: true },
      },
    })
    await page.render()
    await screen.findByText(endUser.email)
    const user = userEvent.setup()

    await user.click(
      within(row(endUser.email)).getByRole('button', { name: 'Open menu' })
    )
    await user.click(await screen.findByRole('menuitem', { name: /Disable/ }))

    expect(post.mock.calls[0][0]).toBe('/api/essera/people/user-1/suspend')
    await waitFor(() =>
      expect(within(row(endUser.email)).getByText('Suspended')).toBeVisible()
    )
    expect(within(row(endUser.email)).getByText('Pending')).toBeVisible()
  })

  it('adds a user who appears at once as pending', async () => {
    const page = peoplePage(listed)
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      status: 201,
      data: {
        success: true,
        data: {
          id: 'new',
          email: 'new@acme.example',
          name: 'New One',
          role: 'user',
          status: 'active',
          key_count: 0,
          pending: true,
        },
      },
    })
    await page.render()
    await screen.findByText(endUser.email)
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Add User' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText('Email'), 'New@acme.example')
    await user.type(within(dialog).getByLabelText('Name'), 'New One')
    await user.click(within(dialog).getByRole('button', { name: 'Create' }))

    await waitFor(() =>
      expect(post).toHaveBeenCalledWith(
        '/api/essera/people',
        { email: 'new@acme.example', name: 'New One' },
        expect.anything()
      )
    )
    expect(await screen.findByText('new@acme.example')).toBeVisible()
    expect(within(row('new@acme.example')).getByText('Pending')).toBeVisible()
  })

  it('says a duplicate email is taken and keeps the dialog open', async () => {
    const page = peoplePage(listed)
    vi.spyOn(api, 'post').mockResolvedValue({
      status: 409,
      data: {
        success: false,
        message: 'A person with this email already exists.',
      },
    })
    await page.render()
    await screen.findByText(endUser.email)
    const user = userEvent.setup()

    await user.click(screen.getByRole('button', { name: 'Add User' }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText('Email'), endUser.email)
    await user.click(within(dialog).getByRole('button', { name: 'Create' }))

    expect(
      await screen.findByText('A person with this email already exists.')
    ).toBeVisible()
    expect(screen.getByRole('dialog')).toBeVisible()
  })

  it('says the data is unavailable when the Console is down', async () => {
    const page = peoplePage({
      body: { success: false, message: 'Data is temporarily unavailable' },
    })
    await page.render()

    expect(
      await screen.findAllByText('Data is temporarily unavailable')
    ).not.toHaveLength(0)
  })
})

describe('The users route on a managed gateway', () => {
  function guard(essera: object | undefined) {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    client.setQueryData(['status'], { essera_managed: true })
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: { id: 2, role: 1, essera } },
    })
    useAuthStore.getState().auth.setUser({
      id: 2,
      username: 'person',
      role: 1,
    } as never)
    const beforeLoad = UsersRoute.options.beforeLoad as (options: {
      context: { queryClient: QueryClient }
    }) => Promise<void>
    return beforeLoad({ context: { queryClient: client } })
  }

  it('lets an admin in', async () => {
    await expect(
      guard({ role: 'admin', available: true })
    ).resolves.toBeUndefined()
  })

  it('sends an end user to 403', async () => {
    const outcome = await guard({ role: 'user', available: true }).catch(
      (error: unknown) => error
    )
    expect(isRedirect(outcome)).toBe(true)
  })
})

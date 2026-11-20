import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { Footer } from '@/components/layout/components/footer'

afterEach(cleanup)

async function renderFooter(status: Record<string, unknown>) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  client.setQueryData(['status'], status)
  const root = createRootRoute({ component: () => <Footer /> })
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('footer on a managed gateway', () => {
  it('keeps the AGPL attribution as one open-source notice and drops the upstream tagline', async () => {
    await renderFooter({ essera_managed: true })

    expect(
      await screen.findByText(/Includes open-source software \(AGPL-3\.0\)/)
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        /Frontend design and development by New API contributors\./
      )
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'New API' })).toHaveAttribute(
      'href',
      'https://github.com/QuantumNous/new-api'
    )
    expect(
      screen.queryByText('Powerful API Management Platform')
    ).not.toBeInTheDocument()
  })

  it('puts the logo and the copyright on the left of one bar and the notice in its own small line', async () => {
    await renderFooter({ essera_managed: true })

    expect(await screen.findByRole('link', { name: 'Home' })).toHaveAttribute(
      'href',
      '/'
    )
    const year = new Date().getFullYear()
    expect(screen.getAllByText(new RegExp(`© ${year} `))[0].tagName).toBe('SPAN')
    expect(
      screen.getByText(/Includes open-source software/).closest('p')
    ).toHaveClass('text-[11px]')
  })
})

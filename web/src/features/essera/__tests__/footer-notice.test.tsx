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
  it('links to the open-source notices instead of showing the attribution and the upstream tagline', async () => {
    await renderFooter({ essera_managed: true })

    expect(
      await screen.findByRole('link', { name: 'Open-source notices' })
    ).toHaveAttribute('href', '/legal')
    expect(
      screen.queryByText(
        /Frontend design and development by New API contributors/
      )
    ).not.toBeInTheDocument()
    expect(
      screen.queryByText('Powerful API Management Platform')
    ).not.toBeInTheDocument()
  })

  it('puts the logo, the copyright and the notices link in one bar', async () => {
    await renderFooter({ essera_managed: true })

    expect(await screen.findByRole('link', { name: 'Home' })).toHaveAttribute(
      'href',
      '/'
    )
    const year = new Date().getFullYear()
    const copyright = screen.getAllByText(new RegExp(`© ${year} `))[0]
    expect(copyright.tagName).toBe('SPAN')
    expect(copyright.parentElement).toContainElement(
      screen.getByRole('link', { name: 'Open-source notices' })
    )
  })
})

describe('footer on a self-hosted gateway', () => {
  it('keeps the new-api attribution and its link', async () => {
    await renderFooter({})

    expect(
      await screen.findByText(
        /Frontend design and development by New API contributors\./
      )
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'New API' })).toHaveAttribute(
      'href',
      'https://github.com/QuantumNous/new-api'
    )
  })
})

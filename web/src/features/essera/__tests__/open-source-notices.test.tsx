import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { OpenSourceNotices } from '@/features/essera/legal/open-source-notices'

afterEach(cleanup)

async function renderNotices() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  client.setQueryData(['status'], { essera_managed: true })
  const root = createRootRoute({ component: OpenSourceNotices })
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: ['/legal'] }),
  })
  await router.load()
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

describe('open-source notices page', () => {
  it('shows the AGPLv3 §7(b) attribution from NOTICE verbatim, with the link to new-api', async () => {
    await renderNotices()

    expect(
      await screen.findByRole('heading', {
        level: 1,
        name: 'Open-source notices',
      })
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        'Frontend design and development by New API contributors.'
      )
    ).toBeInTheDocument()
    expect(
      screen.getByRole('link', {
        name: 'https://github.com/QuantumNous/new-api',
      })
    ).toHaveAttribute('href', 'https://github.com/QuantumNous/new-api')
    expect(screen.getByText(/Original project/)).toBeInTheDocument()
  })

  it('states the AGPL-3.0 license, links its text and marks Essera’s changes', async () => {
    await renderNotices()

    expect(
      await screen.findByText(
        'This service includes software licensed under the GNU Affero General Public License v3.0 (AGPL-3.0).'
      )
    ).toBeInTheDocument()
    expect(
      screen.getByRole('link', { name: 'Read the full AGPL-3.0 text' })
    ).toHaveAttribute('href', 'https://www.gnu.org/licenses/agpl-3.0.html')
    expect(
      screen.getByText(
        'This version has been modified by Essera. Its design, frontend and integrations have been substantially rewritten.'
      )
    ).toBeInTheDocument()
    expect(screen.getByText(/Plus Jakarta Sans/)).toBeInTheDocument()
  })
})

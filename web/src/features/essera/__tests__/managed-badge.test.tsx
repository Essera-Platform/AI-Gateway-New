import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'

import { EsseraManagedBadge } from '../managed-badge'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  window.localStorage.clear()
})

function renderWithStatus(data: Record<string, unknown>) {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url !== '/api/status') throw new Error(`Unexpected request: ${url}`)
    return { data: { success: true, data } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <EsseraManagedBadge />
      </TooltipProvider>
    </QueryClientProvider>
  )
}

describe('Essera managed badge', () => {
  it('names the Console when the status says the gateway is managed', async () => {
    renderWithStatus({ essera_managed: true })

    expect(
      await screen.findByText('Managed by the Essera Operations Console')
    ).toBeInTheDocument()
  })

  it('shows nothing when the gateway is not managed', async () => {
    renderWithStatus({ essera_managed: false, version: 'v1' })

    await vi.waitFor(() => expect(api.get).toHaveBeenCalledWith('/api/status'))
    expect(
      screen.queryByText('Managed by the Essera Operations Console')
    ).not.toBeInTheDocument()
  })
})

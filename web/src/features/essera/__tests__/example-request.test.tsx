import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'
import { api } from '@/lib/api'
import { copyToClipboard } from '@/lib/copy-to-clipboard'

import {
  API_KEY_PLACEHOLDER,
  buildCurlCommand,
  chatCompletionsEndpoint,
} from '../example-request'
import { KeyCreatedDialog } from '../key-created-dialog'

vi.mock('@/lib/copy-to-clipboard', () => ({
  copyToClipboard: vi.fn(async () => true),
}))

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  window.localStorage.clear()
})

describe('Example request', () => {
  it('points at the chat completions route of any base address', () => {
    expect(chatCompletionsEndpoint('https://gateway.example.com/')).toBe(
      'https://gateway.example.com/v1/chat/completions'
    )
    expect(chatCompletionsEndpoint('https://gateway.example.com/v1')).toBe(
      'https://gateway.example.com/v1/chat/completions'
    )
    expect(chatCompletionsEndpoint('')).toBe(
      `${window.location.origin}/v1/chat/completions`
    )
  })

  it('lets the shell fill in the placeholder key', () => {
    const command = buildCurlCommand({
      endpoint: 'https://gateway.example.com/v1/chat/completions',
      apiKey: API_KEY_PLACEHOLDER,
      model: 'claude-sonnet',
    })

    expect(command).toContain(
      '-H "Authorization: Bearer $ESSERA_API_KEY" \\'
    )
    expect(command).toContain('"model":"claude-sonnet"')
  })

  it('copies a ready-to-run request with the key just created', async () => {
    vi.spyOn(api, 'get').mockImplementation(async (url) => {
      if (url === '/api/status') {
        return {
          data: {
            success: true,
            data: { server_address: 'https://gateway.example.com' },
          },
        }
      }
      if (url === '/api/user/models') {
        return { data: { success: true, data: ['claude-sonnet'] } }
      }
      throw new Error(`Unexpected request: ${url}`)
    })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <TooltipProvider>
          <KeyCreatedDialog
            created={{
              id: 1,
              key_id: 'k1',
              key: 'sk-0123456789abcdefSECRET',
              name: 'CI',
              expires_at: '2027-01-01T00:00:00Z',
            }}
            onClose={() => {}}
          />
        </TooltipProvider>
      </QueryClientProvider>
    )
    await vi.waitFor(() => {
      expect(api.get).toHaveBeenCalledWith('/api/status')
      expect(api.get).toHaveBeenCalledWith('/api/user/models')
      expect(client.isFetching()).toBe(0)
    })

    fireEvent.click(
      await screen.findByRole('button', { name: 'Copy example request' })
    )

    await vi.waitFor(() =>
      expect(copyToClipboard).toHaveBeenCalledWith(
        buildCurlCommand({
          endpoint: 'https://gateway.example.com/v1/chat/completions',
          apiKey: 'sk-0123456789abcdefSECRET',
          model: 'claude-sonnet',
        })
      )
    )
  })
})

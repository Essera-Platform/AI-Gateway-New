import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { getUserQuotaDates } from '@/features/dashboard/api'
import type { QuotaDataItem } from '@/features/dashboard/types'

import { LogStatCards } from '../log-stat-cards'

vi.mock('@/features/dashboard/api', () => ({ getUserQuotaDates: vi.fn() }))

afterEach(() => {
  cleanup()
  vi.mocked(getUserQuotaDates).mockReset()
})

function serve(data: QuotaDataItem[]) {
  vi.mocked(getUserQuotaDates).mockResolvedValue({
    success: true,
    data,
  } as Awaited<ReturnType<typeof getUserQuotaDates>>)
}

async function card(title: string) {
  return within(await screen.findByRole('group', { name: title }))
}

describe('the dashboard’s figures for a window', () => {
  it('adds up the cache reads and writes the Console measured', async () => {
    serve([
      {
        created_at: 1795082400,
        token_used: 480,
        cache_read_tokens: 50,
        cache_creation_tokens: 30,
      },
      {
        created_at: 1795086000,
        token_used: 15,
        cache_read_tokens: 7,
        cache_creation_tokens: 0,
      },
    ])
    render(<LogStatCards />)

    expect((await card('Cache Read')).getByText('57')).toBeVisible()
    expect((await card('Cache Write')).getByText('30')).toBeVisible()
  })

  it('gives each other unit measured in the window a card, and none to units without any', async () => {
    serve([
      {
        created_at: 1795082400,
        token_used: 0,
        units: { search_units: 3, images: 0 },
      },
      {
        created_at: 1795086000,
        token_used: 20,
        units: { input_tokens: 20, search_units: 2, input_video_seconds: 90 },
      },
    ])
    render(<LogStatCards />)

    expect((await card('Search unit')).getByText('5')).toBeVisible()
    expect((await card('Video input')).getByText('90 seconds')).toBeVisible()
    expect(screen.queryByRole('group', { name: 'Image' })).toBeNull()
    expect(screen.queryByRole('group', { name: 'Input' })).toBeNull()
  })

  it('shows no unit cards for a gateway whose figures carry no units', async () => {
    serve([{ created_at: 1795082400, token_used: 100, count: 2 }])
    render(<LogStatCards />)

    await card('Total Tokens')
    const titles = screen
      .getAllByRole('group')
      .map((group) => group.getAttribute('aria-label'))
    expect(titles).toEqual([
      'Total Count',
      'Total Quota',
      'Total Tokens',
      'Cache Read',
      'Cache Write',
      'Average RPM',
      'Average TPM',
    ])
  })
})

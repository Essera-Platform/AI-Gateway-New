import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  cleanup,
  render,
  renderHook,
  screen,
  waitFor,
} from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { PromptInput } from '@/components/ai-elements/prompt-input'
import { TooltipProvider } from '@/components/ui/tooltip'
import { SidebarModulesCard } from '@/features/profile/components/sidebar-modules-card'
import { PlaygroundInputTools } from '@/features/playground/components/input/playground-input-tools'
import {
  DEFAULT_CONFIG,
  DEFAULT_PARAMETER_ENABLED,
} from '@/features/playground/constants'
import { useNotifications } from '@/hooks/use-notifications'
import { useTopNavLinks } from '@/hooks/use-top-nav-links'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

beforeEach(() => {
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url === '/api/notice') return { data: { success: true, data: '' } }
    if (url === '/api/user/self') {
      return { data: { success: true, data: { sidebar_modules: '' } } }
    }
    throw new Error(`Unexpected GET ${url}`)
  })
  useAuthStore.getState().auth.setUser({ id: 7, username: 'person', role: 1 })
})

afterEach(() => {
  cleanup()
  useAuthStore.getState().auth.reset()
  vi.restoreAllMocks()
})

function withStatus(status: Record<string, unknown>) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  client.setQueryData(['status'], status)
  return function Wrapper(props: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <TooltipProvider>{props.children}</TooltipProvider>
      </QueryClientProvider>
    )
  }
}

describe('Top navigation on a managed gateway', () => {
  it('links to neither the docs, the About page nor the rankings', () => {
    const { result } = renderHook(() => useTopNavLinks(), {
      wrapper: withStatus({
        essera_managed: true,
        docs_link: 'https://docs.newapi.pro',
      }),
    })

    const titles = result.current.map((link) => link.title)
    expect(titles).not.toContain('Docs')
    expect(titles).not.toContain('About')
    expect(titles).not.toContain('Rankings')
    expect(titles).toEqual(expect.arrayContaining(['Home', 'Console']))
  })

  it('keeps them on an unmanaged gateway', () => {
    const { result } = renderHook(() => useTopNavLinks(), {
      wrapper: withStatus({ docs_link: 'https://docs.newapi.pro' }),
    })

    expect(result.current.map((link) => link.title)).toEqual(
      expect.arrayContaining(['Docs', 'About', 'Rankings'])
    )
  })
})

describe('Notifications on a managed gateway', () => {
  it('are not offered and the notice is never requested', async () => {
    const { result } = renderHook(() => useNotifications(), {
      wrapper: withStatus({ essera_managed: true }),
    })

    expect(result.current.available).toBe(false)
    expect(api.get).not.toHaveBeenCalledWith('/api/notice', expect.anything())
  })

  it('are offered on an unmanaged gateway', async () => {
    const { result } = renderHook(() => useNotifications(), {
      wrapper: withStatus({ essera_managed: false }),
    })

    expect(result.current.available).toBe(true)
    await waitFor(() =>
      expect(api.get).toHaveBeenCalledWith('/api/notice', expect.anything())
    )
  })
})

describe('Playground tools on a managed gateway', () => {
  function renderTools(status: Record<string, unknown>) {
    const Wrapper = withStatus(status)
    render(
      <Wrapper>
        <PromptInput onSubmit={() => undefined}>
          <PlaygroundInputTools
            config={DEFAULT_CONFIG}
            onConfigChange={() => undefined}
            onParameterEnabledChange={() => undefined}
            parameterEnabled={DEFAULT_PARAMETER_ENABLED}
          />
        </PromptInput>
      </Wrapper>
    )
  }

  it('have no Search button and keep Attach', () => {
    renderTools({ essera_managed: true })

    expect(
      screen.queryByRole('button', { name: 'Search' })
    ).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Attach' })).toBeInTheDocument()
  })

  it('keep the Search button on an unmanaged gateway', () => {
    renderTools({ essera_managed: false })

    expect(screen.getByRole('button', { name: 'Search' })).toBeInTheDocument()
  })
})

describe('Sidebar personal settings on a managed gateway', () => {
  it('offer no switch for chat clients, drawing or task logs, or the wallet', async () => {
    const Wrapper = withStatus({ essera_managed: true, SidebarModulesAdmin: '' })
    render(
      <Wrapper>
        <SidebarModulesCard />
      </Wrapper>
    )

    expect(await screen.findByText('Playground')).toBeInTheDocument()
    expect(screen.getByText('Usage Logs')).toBeInTheDocument()
    for (const title of ['Chat', 'Drawing Logs', 'Task Logs', 'Wallet Management']) {
      expect(screen.queryByText(title)).not.toBeInTheDocument()
    }
  })
})

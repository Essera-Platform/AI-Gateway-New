import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, renderHook, screen } from '@testing-library/react'
import i18next from 'i18next'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it } from 'vitest'

import { ConfigDrawer } from '@/components/config-drawer'
import { LanguageSwitcher } from '@/components/language-switcher'
import { useTopNavLinks } from '@/hooks/use-top-nav-links'
import { useAuthStore } from '@/stores/auth-store'

import { LandingBenchmarks } from '../landing/benchmarks'
import { enforceEsseraLanguage } from '../language'

afterEach(async () => {
  cleanup()
  useAuthStore.getState().auth.reset()
  await i18next.changeLanguage('en')
})

function withStatus(status: Record<string, unknown>) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  client.setQueryData(['status'], status)
  return function Wrapper(props: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        {props.children}
      </QueryClientProvider>
    )
  }
}

describe('managed gateway appearance', () => {
  it('offers neither theme settings nor a language choice', () => {
    const { container } = render(
      <>
        <ConfigDrawer />
        <LanguageSwitcher />
      </>,
      { wrapper: withStatus({ essera_managed: true }) }
    )

    expect(container).toBeEmptyDOMElement()
  })

  it('switches the interface to English when the status says managed', async () => {
    await i18next.changeLanguage('fr')

    enforceEsseraLanguage({ essera_managed: true })

    await expect.poll(() => i18next.language).toBe('en')
  })

  it('leaves the language alone on an unmanaged gateway', async () => {
    await i18next.changeLanguage('fr')

    enforceEsseraLanguage({ essera_managed: false })

    expect(i18next.language).toBe('fr')
  })

  it('sends someone signed in Home to the Overview', () => {
    useAuthStore.getState().auth.setUser({ id: 7, username: 'person', role: 1 })

    const { result } = renderHook(() => useTopNavLinks(), {
      wrapper: withStatus({ essera_managed: true }),
    })

    expect(result.current.find((link) => link.title === 'Home')?.href).toBe(
      '/dashboard/overview'
    )
  })

  it('keeps Home on the landing for a guest', () => {
    const { result } = renderHook(() => useTopNavLinks(), {
      wrapper: withStatus({ essera_managed: true }),
    })

    expect(result.current.find((link) => link.title === 'Home')?.href).toBe('/')
  })
})

describe('landing benchmarks', () => {
  it('shows the live model and family counts from the status', () => {
    render(<LandingBenchmarks />, {
      wrapper: withStatus({
        essera_managed: true,
        essera_catalog: { models: 42, families: 9 },
      }),
    })

    expect(screen.getByText('42')).toBeInTheDocument()
    expect(screen.getByText('9')).toBeInTheDocument()
    expect(screen.getByText('Models available')).toBeInTheDocument()
    expect(screen.getByText('Model families')).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument()
    expect(screen.getByText('Per account')).toBeInTheDocument()
  })

  it('shows a dash, not a made-up figure, before the catalog is known', () => {
    render(<LandingBenchmarks />, {
      wrapper: withStatus({ essera_managed: true }),
    })

    expect(screen.getAllByText('—')).toHaveLength(2)
  })
})

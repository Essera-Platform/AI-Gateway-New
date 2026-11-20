import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, renderHook, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { ThemeProvider } from '@/context/theme-provider'
import { useSidebarConfig } from '@/hooks/use-sidebar-config'
import { useSidebarData } from '@/hooks/use-sidebar-data'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

import { ApiDocs, OPENAPI_DOCUMENT_URL } from '../api-docs'

const viewer = vi.hoisted(() => ({
  configurations: [] as Record<string, unknown>[],
}))

vi.mock('@scalar/api-reference-react', () => ({
  ApiReferenceReact: (props: { configuration: Record<string, unknown> }) => {
    viewer.configurations.push(props.configuration)
    const content = props.configuration.content as {
      info: { title: string }
    }
    return <h1>{content.info.title}</h1>
  },
}))

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  viewer.configurations.length = 0
  useAuthStore.getState().auth.reset()
  window.localStorage.clear()
})

function wrapperWithStatus(status: Record<string, unknown>) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
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

function generalItems(status: Record<string, unknown>) {
  useAuthStore.getState().auth.setUser({
    id: 7,
    username: 'person',
    role: 1,
    sidebar_modules: '',
  })
  const { result } = renderHook(
    () => useSidebarConfig(useSidebarData().navGroups),
    { wrapper: wrapperWithStatus(status) }
  )
  return result.current.find((group) => group.id === 'general')?.items ?? []
}

describe('API Docs in the console sidebar', () => {
  it('a managed gateway lists API Docs right after Audit Logs', () => {
    const items = generalItems({ essera_managed: true })
    const audit = items.findIndex((item) => item.title === 'Audit Logs')

    expect(audit).toBeGreaterThanOrEqual(0)
    expect(items[audit + 1]).toMatchObject({
      title: 'API Docs',
      url: '/docs',
    })
  })

  it('a gateway the Console does not administer has no API Docs entry', () => {
    const titles = generalItems({ essera_managed: false }).map(
      (item) => item.title
    )

    expect(titles).not.toContain('API Docs')
  })
})

describe('API Docs page', () => {
  it('shows the document the Gateway serves, calling only the Gateway and storing no key', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      data: {
        openapi: '3.1.0',
        info: { title: 'Essera AI Gateway API', version: '1.0' },
        servers: [{ url: 'https://ai-gateway-dev.esseratech.com' }],
        paths: {},
      },
    })
    const Wrapper = wrapperWithStatus({ essera_managed: true })

    render(
      <Wrapper>
        <ThemeProvider defaultTheme='dark'>
          <ApiDocs />
        </ThemeProvider>
      </Wrapper>
    )

    expect(
      await screen.findByRole('heading', { name: 'Essera AI Gateway API' })
    ).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'API Docs' })).toBeTruthy()
    expect(get).toHaveBeenCalledWith(OPENAPI_DOCUMENT_URL)
    const configuration = viewer.configurations.at(-1)
    expect(configuration).toMatchObject({
      forceDarkModeState: 'dark',
      persistAuth: false,
      telemetry: false,
      withDefaultFonts: false,
      agent: { disabled: true },
      mcp: { disabled: true },
    })
    expect(configuration).not.toHaveProperty('proxyUrl')
  })

  it('offers a retry when the document cannot be loaded', async () => {
    vi.spyOn(api, 'get').mockRejectedValue(new Error('offline'))
    const Wrapper = wrapperWithStatus({ essera_managed: true })

    render(
      <Wrapper>
        <ApiDocs />
      </Wrapper>
    )

    expect(
      await screen.findByText('The API documentation could not be loaded.')
    ).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy()
    expect(viewer.configurations).toHaveLength(0)
  })
})

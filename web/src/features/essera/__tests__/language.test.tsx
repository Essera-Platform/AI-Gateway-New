import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import type { ReactNode } from 'react'
import { afterEach, beforeAll, describe, expect, it } from 'vitest'

import { LanguageSwitcher } from '@/components/language-switcher'
import en from '@/i18n/locales/en.json'
import zh from '@/i18n/locales/zh.json'

import { enforceEsseraLanguage } from '../language'
import {
  NEW_API_ATTRIBUTION,
  OpenSourceNotices,
} from '../legal/open-source-notices'

beforeAll(() => {
  i18next.addResourceBundle('zhCN', 'translation', zh.translation)
})

afterEach(async () => {
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

describe('managed gateway language', () => {
  it('reads English on the first status when the browser chose no offered language', async () => {
    await i18next.changeLanguage('fr')

    enforceEsseraLanguage({ essera_managed: true })

    await expect.poll(() => i18next.language).toBe('en')
  })

  it('keeps 中文 once it is chosen', async () => {
    await i18next.changeLanguage('zhCN')

    enforceEsseraLanguage({ essera_managed: true })

    expect(i18next.language).toBe('zhCN')
  })

  it('offers exactly English and 中文 in the switcher and switches to Chinese', async () => {
    const user = userEvent.setup()
    render(<LanguageSwitcher />, {
      wrapper: withStatus({ essera_managed: true }),
    })

    await user.click(screen.getByRole('button', { name: 'Change language' }))
    const items = await screen.findAllByRole('menuitem')
    expect(items.map((item) => item.textContent)).toEqual(['English', '中文'])

    await user.click(screen.getByRole('menuitem', { name: '中文' }))

    await expect.poll(() => i18next.language).toBe('zhCN')
    expect(screen.getByRole('button', { name: '更改语言' })).toBeInTheDocument()
  })
})

describe('Chinese locale', () => {
  it('has a Chinese string for every English one', () => {
    const missing = Object.keys(en.translation).filter(
      (key) => !(key in zh.translation)
    )

    expect(missing).toEqual([])
  })

  it('keeps the AGPLv3 §7(b) attribution in English on the Chinese notices page, with a gloss', async () => {
    await i18next.changeLanguage('zhCN')
    const root = createRootRoute({ component: OpenSourceNotices })
    const router = createRouter({
      routeTree: root,
      history: createMemoryHistory({ initialEntries: ['/legal'] }),
    })
    await router.load()
    render(<RouterProvider router={router} />, {
      wrapper: withStatus({ essera_managed: true }),
    })

    expect(
      await screen.findByRole('heading', { level: 1, name: '开源声明' })
    ).toBeInTheDocument()
    expect(screen.getByText(NEW_API_ATTRIBUTION)).toBeInTheDocument()
    expect(
      screen.getByText('（中文释义：前端设计与开发由 New API 贡献者完成。）')
    ).toBeInTheDocument()
  })
})

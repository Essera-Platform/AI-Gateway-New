import { describe, expect, it } from 'vitest'

import {
  applyManagedSidebarModules,
  isManagedClosedPath,
  isManagedClosedPublicPath,
} from '../menu'

describe('Pages the Console owns on a managed gateway', () => {
  it.each([
    ['/wallet', true],
    ['/wallet/history', true],
    ['/redemption-codes', true],
    ['/subscriptions', true],
    ['/usage-logs/task', true],
    ['/usage-logs/drawing', true],
    ['/chat/0', true],
    ['/chat2link', true],
    ['/playground', false],
    ['/usage-logs/common', false],
    ['/usage-logs/audit', false],
    ['/walletx', false],
    ['/keys', false],
  ])('%s closed: %s', (path, closed) => {
    expect(isManagedClosedPath(path)).toBe(closed)
  })
})

describe('Public pages a managed gateway does not serve', () => {
  it.each([
    ['/about', true],
    ['/rankings', true],
    ['/sign-up', true],
    ['/register', true],
    ['/forgot-password', true],
    ['/reset', true],
    ['/user/reset', true],
    ['/oauth/github', true],
    ['/sign-in', false],
    ['/pricing', false],
    ['/docs', false],
    ['/', false],
    ['/aboutx', false],
  ])('%s closed: %s', (path, closed) => {
    expect(isManagedClosedPublicPath(path)).toBe(closed)
  })
})

describe('Sidebar modules a managed gateway never shows', () => {
  it('switches off the chat clients and keeps the playground', () => {
    const config = applyManagedSidebarModules({
      chat: { enabled: true, playground: true, chat: true },
      personal: { enabled: true, topup: true, personal: true },
    })

    expect(config.chat).toEqual({ enabled: true, playground: true, chat: false })
    expect(config.personal.topup).toBe(false)
    expect(config.personal.personal).toBe(true)
  })
})

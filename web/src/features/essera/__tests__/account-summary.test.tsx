import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'

import { ProfileHeader } from '@/features/profile/components/profile-header'
import type { UserProfile } from '@/features/profile/types'

import type { EsseraAccount } from '../types'

afterEach(cleanup)

function profile(essera?: EsseraAccount): UserProfile {
  return {
    id: 7,
    username: 'person',
    display_name: 'Person',
    role: 1,
    group: 'essera',
    quota: 0,
    used_quota: 0,
    request_count: 0,
    status: 1,
    aff_count: 0,
    aff_quota: 0,
    essera,
  } as UserProfile
}

const account: EsseraAccount = {
  role: 'user',
  available: true,
  customer_name: 'Helios',
  currency: 'USD',
  spend: '12.34',
  spend_limit: '100.00',
  spend_left: '87.66',
  requests: 120,
  request_quota: null,
  tokens: 45000,
  token_quota: null,
}

// A figure is rendered with its unit in a separate span, so match the whole figure's text.
const figure = (text: string) => (_: string, element: Element | null) =>
  element?.classList.contains('num') === true && element.textContent === text

describe('account summary on a managed gateway', () => {
  it('shows this month’s spend and the spend limit instead of a balance', () => {
    render(<ProfileHeader profile={profile(account)} loading={false} />)

    expect(screen.getByText('Spend this month')).toBeVisible()
    expect(screen.getByText(figure('$12.34'))).toBeVisible()
    expect(screen.getByText('Spend limit')).toBeVisible()
    expect(screen.getByText(figure('$100.00'))).toBeVisible()
    expect(screen.queryByText('Current Balance')).not.toBeInTheDocument()
    expect(screen.queryByText('essera')).not.toBeInTheDocument()
  })

  it('says there is no limit when the Console sets none', () => {
    render(
      <ProfileHeader
        profile={profile({ ...account, spend_limit: null, spend_left: null })}
        loading={false}
      />
    )

    expect(screen.getByText('No limit')).toBeVisible()
  })

  it('says the data is unavailable when the Console did not answer', () => {
    render(
      <ProfileHeader
        profile={profile({ ...account, available: false })}
        loading={false}
      />
    )

    expect(
      screen.getAllByText('Data is temporarily unavailable').length
    ).toBeGreaterThan(0)
    expect(screen.queryByText('$12.34')).not.toBeInTheDocument()
  })

  it('keeps the balance for a user the Console does not know', () => {
    render(<ProfileHeader profile={profile()} loading={false} />)

    expect(screen.getByText('Current Balance')).toBeVisible()
  })
})

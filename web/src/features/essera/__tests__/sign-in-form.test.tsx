import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from '@tanstack/react-router'
import { act, cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AxiosError, AxiosHeaders } from 'axios'
import { Toaster, toast } from 'sonner'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { useAuthStore, type AuthBundle } from '@/stores/auth-store'

import { EsseraSignInForm } from '../sign-in-form'

const bundle: AuthBundle = {
  access_token: 'person-session',
  token_type: 'Bearer',
  access_expires_at: 9999999999,
  user: { id: 42, username: 'user-1@acme.example', role: 1 },
  session: {
    sid: 'person-sid',
    current: true,
    login_method: 'essera',
    ip: '',
    user_agent: '',
    created_at: 1,
    last_active_at: 1,
    expires_at: 9999999999,
  },
}

function refusal(status: number, message: string) {
  return new AxiosError('Request failed', 'ERR_BAD_REQUEST', undefined, null, {
    status,
    statusText: '',
    headers: {},
    config: { headers: new AxiosHeaders() },
    data: { success: false, message },
  })
}

const EMAIL = 'user-1@acme.example'
const EXPIRED = 'The code has expired. Send a new code.'

function nextStep(next: 'code' | 'new_password', expiresIn = 180) {
  return { data: { success: true, data: { next, expires_in: expiresIn } } }
}

function renderForm(signInMode: 'password' | 'email_otp' = 'email_otp') {
  const status = { essera_managed: true, essera_sign_in: signInMode }
  vi.spyOn(api, 'get').mockImplementation(async (url) => {
    if (url !== '/api/status') throw new Error(`Unexpected request: ${url}`)
    return { data: { success: true, data: status } }
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  client.setQueryData(['status'], status)
  const onOperatorSignIn = vi.fn()
  const root = createRootRoute({ component: Outlet })
  const router = createRouter({
    routeTree: root.addChildren([
      createRoute({
        getParentRoute: () => root,
        path: '/sign-in',
        component: () => (
          <EsseraSignInForm onOperatorSignIn={onOperatorSignIn} />
        ),
      }),
      createRoute({
        getParentRoute: () => root,
        path: '/dashboard',
        component: () => <div>Dashboard destination</div>,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: ['/sign-in'] }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
      <Toaster />
    </QueryClientProvider>
  )
  return { onOperatorSignIn }
}

beforeEach(() => {
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
  localStorage.clear()
  useAuthStore.getState().auth.reset('complete')
})

afterEach(() => {
  cleanup()
  toast.dismiss()
  vi.useRealTimers()
  vi.restoreAllMocks()
  localStorage.clear()
  useAuthStore.getState().auth.reset('idle')
})

async function requestCode(user: ReturnType<typeof userEvent.setup>) {
  await user.type(await screen.findByLabelText('Email'), EMAIL)
  await user.click(screen.getByRole('button', { name: 'Continue' }))
  return screen.findByLabelText('Verification code')
}

describe('Essera sign-in by emailed code', () => {
  it('asks only for the email, names it on the code step and signs in with the code', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValueOnce(nextStep('code'))
      .mockResolvedValueOnce({ data: { success: true, data: bundle } })
    renderForm()
    const user = userEvent.setup()

    expect(await screen.findByLabelText('Email')).toBeInTheDocument()
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument()
    const code = await requestCode(user)
    expect(code).toHaveAttribute('autocomplete', 'one-time-code')
    expect(
      screen.getByText(`Enter the code we sent to ${EMAIL}.`)
    ).toBeInTheDocument()
    expect(screen.getByRole('timer')).toHaveTextContent(
      'The code expires in 3:00.'
    )
    await user.type(code, '12345678')
    await user.click(screen.getByRole('button', { name: 'Continue' }))

    expect(await screen.findByText('Dashboard destination')).toBeInTheDocument()
    expect(post).toHaveBeenNthCalledWith(
      1,
      '/api/essera/sign-in',
      { email: EMAIL },
      { skipAuthRefresh: true }
    )
    expect(post).toHaveBeenNthCalledWith(
      2,
      '/api/essera/sign-in/continue',
      { code: '12345678' },
      { skipAuthRefresh: true }
    )
    expect(useAuthStore.getState().auth.session?.sid).toBe('person-sid')
  })

  it('counts the code down and disables Continue once it has expired', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    vi.spyOn(api, 'post').mockResolvedValueOnce(nextStep('code', 120))
    renderForm()
    const user = userEvent.setup()

    await requestCode(user)
    expect(screen.getByRole('timer')).toHaveTextContent('2:00')
    act(() => vi.advanceTimersByTime(61_000))
    expect(screen.getByRole('timer')).toHaveTextContent('0:59')
    act(() => vi.advanceTimersByTime(59_000))

    expect(screen.getByRole('alert')).toHaveTextContent(EXPIRED)
    expect(screen.queryByRole('timer')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Continue' })).toBeDisabled()
  })

  it('sends a new code for the same email only after the cooldown and restarts the countdown', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValueOnce(nextStep('code', 180))
      .mockResolvedValueOnce(nextStep('code', 120))
    renderForm()
    const user = userEvent.setup()

    await requestCode(user)
    expect(
      screen.getByRole('button', { name: 'Send a new code in 30 s' })
    ).toBeDisabled()
    act(() => vi.advanceTimersByTime(30_000))
    await user.click(screen.getByRole('button', { name: 'Send a new code' }))

    await waitFor(() => expect(post).toHaveBeenCalledTimes(2))
    expect(post).toHaveBeenLastCalledWith(
      '/api/essera/sign-in',
      { email: EMAIL },
      { skipAuthRefresh: true }
    )
    expect(await screen.findByRole('timer')).toHaveTextContent('2:00')
    expect(
      screen.getByRole('button', { name: 'Send a new code in 30 s' })
    ).toBeDisabled()
    expect(screen.getByLabelText('Verification code')).toHaveValue('')
  })

  it('stays on the code step with the resend button when the server says the code expired', async () => {
    vi.spyOn(api, 'post')
      .mockResolvedValueOnce(nextStep('code'))
      .mockRejectedValueOnce(refusal(401, EXPIRED))
    renderForm()
    const user = userEvent.setup()

    await user.type(await requestCode(user), '12345678')
    await user.click(screen.getByRole('button', { name: 'Continue' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(EXPIRED)
    expect(screen.getByLabelText('Verification code')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Continue' })).toBeDisabled()
    expect(
      screen.getByRole('button', { name: /Send a new code/ })
    ).toBeInTheDocument()
  })

  it('keeps the code step and shows the throttling message on 429', async () => {
    vi.spyOn(api, 'post')
      .mockResolvedValueOnce(nextStep('code'))
      .mockRejectedValueOnce(
        refusal(429, 'Too many sign-in attempts. Try again later.')
      )
    renderForm()
    const user = userEvent.setup()

    await user.type(await requestCode(user), '12345678')
    await user.click(screen.getByRole('button', { name: 'Continue' }))

    expect(
      await screen.findByText('Too many sign-in attempts. Try again later.')
    ).toBeInTheDocument()
    expect(screen.getByLabelText('Verification code')).toBeInTheDocument()
  })

  it('returns to the email step with the generic message when a code is refused', async () => {
    vi.spyOn(api, 'post')
      .mockResolvedValueOnce(nextStep('code'))
      .mockRejectedValueOnce(
        refusal(401, 'The email, password or code is not correct.')
      )
    renderForm()
    const user = userEvent.setup()

    await user.type(await requestCode(user), '000000')
    await user.click(screen.getByRole('button', { name: 'Continue' }))

    expect(
      await screen.findByText('The email, password or code is not correct.')
    ).toBeInTheDocument()
    await waitFor(() =>
      expect(
        screen.queryByLabelText('Verification code')
      ).not.toBeInTheDocument()
    )
    expect(screen.getByLabelText('Email')).toBeInTheDocument()
    expect(useAuthStore.getState().auth.session).toBeFalsy()
  })

  it('goes back to the email step through "Use a different email"', async () => {
    vi.spyOn(api, 'post').mockResolvedValueOnce(nextStep('code'))
    renderForm()
    const user = userEvent.setup()

    await requestCode(user)
    await user.click(
      screen.getByRole('button', { name: 'Use a different email' })
    )

    expect(await screen.findByLabelText('Email')).toBeInTheDocument()
    expect(screen.queryByLabelText('Verification code')).not.toBeInTheDocument()
  })

  it('hands over to the operator sign-in from the first step', async () => {
    const { onOperatorSignIn } = renderForm()

    await userEvent.click(
      await screen.findByRole('button', { name: 'Operator sign-in' })
    )

    expect(onOperatorSignIn).toHaveBeenCalledTimes(1)
  })
})

describe('Essera sign-in by password', () => {
  it('signs a person in with email and password and opens the dashboard', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ data: { success: true, data: bundle } })
    renderForm('password')
    const user = userEvent.setup()

    await user.type(await screen.findByLabelText('Email'), EMAIL)
    await user.type(screen.getByLabelText('Password'), 'Correct-Horse-1')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(await screen.findByText('Dashboard destination')).toBeInTheDocument()
    expect(post).toHaveBeenCalledWith(
      '/api/essera/sign-in',
      { email: EMAIL, password: 'Correct-Horse-1' },
      { skipAuthRefresh: true }
    )
  })

  it('asks for a new password on the first sign-in and keeps the step when the policy refuses it', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValueOnce(nextStep('new_password'))
      .mockResolvedValueOnce({
        data: {
          success: false,
          message: 'The new password does not meet the password policy.',
          data: { next: 'new_password' },
        },
      })
      .mockResolvedValueOnce({ data: { success: true, data: bundle } })
    renderForm('password')
    const user = userEvent.setup()

    await user.type(await screen.findByLabelText('Email'), 'owner@acme.example')
    await user.type(screen.getByLabelText('Password'), 'Temporary-1')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    await user.type(await screen.findByLabelText('New password'), 'weakpass')
    await user.type(screen.getByLabelText('Confirm new password'), 'weakpass')
    await user.click(screen.getByRole('button', { name: 'Continue' }))

    expect(
      await screen.findByText(
        'The new password does not meet the password policy.'
      )
    ).toBeInTheDocument()
    const newPassword = screen.getByLabelText('New password')
    await user.clear(newPassword)
    await user.type(newPassword, 'Another-Horse-22')
    const confirm = screen.getByLabelText('Confirm new password')
    await user.clear(confirm)
    await user.type(confirm, 'Another-Horse-22')
    await user.click(screen.getByRole('button', { name: 'Continue' }))

    expect(await screen.findByText('Dashboard destination')).toBeInTheDocument()
    expect(post).toHaveBeenLastCalledWith(
      '/api/essera/sign-in/continue',
      { new_password: 'Another-Horse-22' },
      { skipAuthRefresh: true }
    )
  })

  it('refuses a new password that differs from its confirmation without calling the server', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValueOnce(nextStep('new_password'))
    renderForm('password')
    const user = userEvent.setup()

    await user.type(await screen.findByLabelText('Email'), 'owner@acme.example')
    await user.type(screen.getByLabelText('Password'), 'Temporary-1')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    await user.type(await screen.findByLabelText('New password'), 'Another-1')
    await user.type(screen.getByLabelText('Confirm new password'), 'Another-2')
    await user.click(screen.getByRole('button', { name: 'Continue' }))

    expect(
      await screen.findByText('Passwords do not match')
    ).toBeInTheDocument()
    expect(post).toHaveBeenCalledTimes(1)
  })

  it('keeps the email step and clears the password when sign-in is throttled', async () => {
    vi.spyOn(api, 'post').mockRejectedValueOnce(
      refusal(429, 'Too many sign-in attempts. Try again later.')
    )
    renderForm('password')
    const user = userEvent.setup()

    await user.type(await screen.findByLabelText('Email'), EMAIL)
    await user.type(screen.getByLabelText('Password'), 'wrong')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))

    expect(
      await screen.findByText('Too many sign-in attempts. Try again later.')
    ).toBeInTheDocument()
    expect(screen.getByLabelText('Password')).toHaveValue('')
  })
})

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { TooltipProvider } from '@/components/ui/tooltip'
import { PlaygroundInput } from '@/features/playground/components/input/playground-input'
import {
  DEFAULT_CONFIG,
  DEFAULT_PARAMETER_ENABLED,
} from '@/features/playground/constants'

const toastError = vi.hoisted(() => vi.fn())

vi.mock('sonner', () => ({
  toast: Object.assign(vi.fn(), {
    error: toastError,
    info: vi.fn(),
    success: vi.fn(),
  }),
}))

vi.mock('@/features/essera/use-essera-managed', () => ({
  useEsseraManaged: () => ({ managed: true, signInMode: 'email_otp' }),
}))

function renderInput(onSubmit = vi.fn()) {
  render(
    <TooltipProvider>
      <PlaygroundInput
        config={{ ...DEFAULT_CONFIG, model: 'nova-lite' }}
        groups={[{ label: 'essera', value: 'essera', ratio: 1 }]}
        groupValue='essera'
        modelValue='nova-lite'
        models={[{ label: 'nova-lite', value: 'nova-lite' }]}
        onConfigChange={vi.fn()}
        onGroupChange={vi.fn()}
        onModelChange={vi.fn()}
        onParameterEnabledChange={vi.fn()}
        onSubmit={onSubmit}
        parameterEnabled={DEFAULT_PARAMETER_ENABLED}
      />
    </TooltipProvider>
  )
  return onSubmit
}

// PromptInput reads each blob URL back with fetch on submit; Node's fetch
// would return a Blob that jsdom's FileReader does not take.
function uploadPhoto(file: File) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => ({ blob: async () => file }))
  )
  return userEvent.upload(screen.getByLabelText('Upload photo'), file)
}

describe('Attaching files in the playground', () => {
  beforeEach(() => {
    toastError.mockReset()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    Reflect.deleteProperty(navigator, 'mediaDevices')
  })

  it('shows an uploaded photo as a chip that can be removed', async () => {
    renderInput()

    await uploadPhoto(new File(['png'], 'cat.png', { type: 'image/png' }))

    expect(screen.getByText('cat.png')).toBeInTheDocument()
    await userEvent.click(
      screen.getByRole('button', { name: 'Remove attachment' })
    )
    expect(screen.queryByText('cat.png')).not.toBeInTheDocument()
  })

  it('refuses a photo over 3.75 MB with a message naming it', async () => {
    renderInput()
    const big = new File(['png'], 'huge.png', { type: 'image/png' })
    Object.defineProperty(big, 'size', { value: 4 * 1024 * 1024 })

    await uploadPhoto(big)

    expect(screen.queryByText('huge.png')).not.toBeInTheDocument()
    expect(toastError).toHaveBeenCalledWith('huge.png is larger than 3.75 MB')
  })

  it('sends the prompt with the attachment as a data URL', async () => {
    const onSubmit = renderInput()
    await uploadPhoto(new File(['png'], 'cat.png', { type: 'image/png' }))

    await userEvent.type(
      screen.getByPlaceholderText('Ask anything'),
      'What is this?{Enter}'
    )

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1))
    const [text, attachments] = onSubmit.mock.calls[0]
    expect(text).toBe('What is this?')
    expect(attachments).toMatchObject([
      {
        kind: 'image',
        name: 'cat.png',
        mediaType: 'image/png',
        url: `data:image/png;base64,${btoa('png')}`,
      },
    ])
  })

  it('keeps the attachment when there is no prompt to send', async () => {
    const onSubmit = renderInput()
    await uploadPhoto(new File(['png'], 'cat.png', { type: 'image/png' }))

    await userEvent.type(screen.getByPlaceholderText('Ask anything'), '{Enter}')

    await waitFor(() => expect(screen.getByText('cat.png')).toBeInTheDocument())
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('offers screenshot and photo only where the browser can take them', async () => {
    renderInput()
    await userEvent.click(screen.getByRole('button', { name: 'Attach' }))

    expect(
      screen.getByRole('menuitem', { name: 'Upload file' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('menuitem', { name: 'Upload photo' })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('menuitem', { name: 'Take screenshot' })
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('menuitem', { name: 'Take photo' })
    ).not.toBeInTheDocument()
  })

  it('offers screenshot and photo when the browser has the media APIs', async () => {
    Object.defineProperty(navigator, 'mediaDevices', {
      configurable: true,
      value: { getDisplayMedia: vi.fn(), getUserMedia: vi.fn() },
    })
    renderInput()
    await userEvent.click(screen.getByRole('button', { name: 'Attach' }))

    expect(
      screen.getByRole('menuitem', { name: 'Take screenshot' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('menuitem', { name: 'Take photo' })
    ).toBeInTheDocument()
  })
})

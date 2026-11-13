/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { TransportItem } from '@grafana/faro-web-sdk'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  initializeFaro: vi.fn(),
  getWebInstrumentations: vi.fn(() => []),
  pushError: vi.fn(),
}))

vi.mock('@grafana/faro-web-sdk', () => ({
  initializeFaro: mocks.initializeFaro,
  getWebInstrumentations: mocks.getWebInstrumentations,
}))
vi.mock('@grafana/faro-web-tracing', () => ({
  TracingInstrumentation: class {},
}))

beforeEach(() => {
  vi.resetModules()
  vi.clearAllMocks()
  vi.stubEnv('PROD', true)
  mocks.initializeFaro.mockReturnValue({ api: { pushError: mocks.pushError } })
})

afterEach(() => {
  vi.unstubAllEnvs()
})

describe('frontend observability', () => {
  it('initializes production Faro once for concurrent callers without console capture', async () => {
    const { initializeFrontendObservability } = await import('../observability')
    const [first, second] = await Promise.all([
      initializeFrontendObservability(),
      initializeFrontendObservability(),
    ])

    expect(first).toBe(second)
    expect(mocks.initializeFaro).toHaveBeenCalledTimes(1)
    expect(mocks.initializeFaro).toHaveBeenCalledWith(
      expect.objectContaining({
        url: 'https://faro-collector-prod-us-east-3.grafana.net/collect/6c545e219edd05c45301e5eeadec030e',
        app: expect.objectContaining({
          name: 'ai-gateway-dev-frontend',
          environment: 'production',
        }),
        beforeSend: expect.any(Function),
      })
    )
    expect(mocks.getWebInstrumentations).toHaveBeenCalledWith({
      captureConsole: false,
    })
  })

  it('does not initialize Faro when running a development build', async () => {
    vi.stubEnv('PROD', false)
    const { initializeFrontendObservability } = await import('../observability')

    await expect(initializeFrontendObservability()).resolves.toBeUndefined()
    expect(mocks.initializeFaro).not.toHaveBeenCalled()
  })

  it('keeps the application running when SDK initialization fails', async () => {
    mocks.initializeFaro.mockImplementationOnce(() => {
      throw new Error('collector unavailable')
    })
    const { initializeFrontendObservability } = await import('../observability')

    await expect(initializeFrontendObservability()).resolves.toBeUndefined()
  })

  it('reports caught framework errors without serializing arbitrary error objects', async () => {
    const { initializeFrontendObservability, reportFrontendError } =
      await import('../observability')
    await initializeFrontendObservability()
    const error = new Error('component failed')

    reportFrontendError({ request: { password: 'private-value' } })
    reportFrontendError(error)
    await initializeFrontendObservability()

    expect(mocks.pushError).toHaveBeenCalledExactlyOnceWith(error)
  })

  it('removes URL credentials, queries, fragments and API tokens across outgoing signals', async () => {
    const { sanitizeFrontendTelemetry } = await import('../observability')
    const item = {
      type: 'event',
      meta: {
        page: {
          url: 'https://app.example/api/oauth/callback?code=private-code#private-fragment',
        },
      },
      payload: {
        name: 'request',
        attributes: {
          message:
            'Failed https://user:private-password@api.example/v1/test?key=private-key',
          token: 'sk-private-token',
          spans: [
            { url: 'https://api.example/tasks?access_token=private-token' },
          ],
        },
      },
    } as unknown as TransportItem

    const sanitized = sanitizeFrontendTelemetry(item)

    expect(sanitized.meta.page?.url).toBe(
      'https://app.example/api/oauth/callback'
    )
    expect(sanitized.payload).toMatchObject({
      attributes: {
        message: 'Failed https://api.example/v1/test',
        token: '[redacted token]',
        spans: [{ url: 'https://api.example/tasks' }],
      },
    })
    expect(JSON.stringify(sanitized)).not.toContain('private-')
    expect(item.meta.page?.url).toContain('private-code')
  })

  it('preserves OTLP binary identifiers while redacting HTTP span attributes', async () => {
    const { sanitizeFrontendTelemetry } = await import('../observability')
    const traceId = new Uint8Array([1, 2, 3, 4])
    const item = {
      type: 'trace',
      meta: {},
      payload: {
        resourceSpans: [
          {
            scopeSpans: [
              {
                spans: [
                  {
                    traceId,
                    attributes: [
                      {
                        key: 'http.url',
                        value: {
                          stringValue:
                            'https://api.example/test?token=private-value',
                        },
                      },
                    ],
                  },
                ],
              },
            ],
          },
        ],
      },
    } as unknown as TransportItem

    expect(sanitizeFrontendTelemetry(item).payload).toMatchObject({
      resourceSpans: [
        {
          scopeSpans: [
            {
              spans: [
                {
                  traceId,
                  attributes: [
                    {
                      key: 'http.url',
                      value: { stringValue: 'https://api.example/test' },
                    },
                  ],
                },
              ],
            },
          ],
        },
      ],
    })
  })
})

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
import type { Faro, TransportItem } from '@grafana/faro-web-sdk'

import { getBuildRevision } from '@/lib/build-metadata'

let initialization: Promise<Faro | undefined> | undefined

// Apply the same URL redaction to page metadata, HTTP spans, events and error
// messages. In particular, OAuth codes and signed artifact URLs must not leave
// the browser in telemetry. Do not enable automatic console or header capture.
function redactTelemetryValue(value: unknown): unknown {
  if (typeof value === 'string') {
    return value
      .replaceAll(/https?:\/\/[^\s"'<>]+/g, (rawUrl) => {
        try {
          const url = new URL(rawUrl)
          url.username = ''
          url.password = ''
          url.search = ''
          url.hash = ''
          return url.toString()
        } catch {
          return '[redacted URL]'
        }
      })
      .replaceAll(/\b(?:sk-|glc_|glsa_)[A-Za-z0-9_-]+/g, '[redacted token]')
  }
  if (Array.isArray(value)) return value.map(redactTelemetryValue)
  if (value instanceof Uint8Array) return value
  if (value !== null && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value).map(([key, entry]) => [
        key,
        redactTelemetryValue(entry),
      ])
    )
  }
  return value
}

export function sanitizeFrontendTelemetry(item: TransportItem): TransportItem {
  return redactTelemetryValue(item) as TransportItem
}

// Load third-party instrumentation separately from the initial React bundle.
// This promise also prevents duplicate SDK instances during concurrent reports.
export function initializeFrontendObservability(): Promise<Faro | undefined> {
  if (!import.meta.env.PROD) return Promise.resolve(undefined)

  initialization ??= Promise.all([
    import('@grafana/faro-web-sdk'),
    import('@grafana/faro-web-tracing'),
  ])
    .then(([sdk, tracing]) =>
      sdk.initializeFaro({
        url: 'https://faro-collector-prod-us-east-3.grafana.net/collect/6c545e219edd05c45301e5eeadec030e',
        app: {
          name: 'ai-gateway-dev-frontend',
          version: getBuildRevision(),
          environment: 'production',
        },
        beforeSend: sanitizeFrontendTelemetry,
        instrumentations: [
          ...sdk.getWebInstrumentations({ captureConsole: false }),
          new tracing.TracingInstrumentation(),
        ],
      })
    )
    .catch(() => undefined)

  return initialization
}

// React and TanStack Router catch errors before window.onerror can see them.
export function reportFrontendError(error: unknown): void {
  if (!(error instanceof Error)) return
  void initializeFrontendObservability().then((faro) => {
    faro?.api.pushError(error)
  })
}

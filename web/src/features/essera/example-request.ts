import { useStatus } from '@/hooks/use-status'
import { getUserModels } from '@/lib/api'
import { requireServerSuccess } from '@/lib/server-error-message'

// A managed gateway never reads a key back after creating it, so its example names a shell
// variable the person sets to their key.
export const API_KEY_PLACEHOLDER = '$ESSERA_API_KEY'

export const FALLBACK_EXAMPLE_MODEL = 'gpt-4o-mini'

export const userModelsQueryOptions = {
  queryKey: ['dashboard', 'overview', 'user-models'],
  queryFn: async (): Promise<string[]> => {
    const result = requireServerSuccess(await getUserModels())
    return result.success ? (result.data ?? []) : []
  },
  staleTime: 5 * 60 * 1000,
}

export function chatCompletionsEndpoint(baseUrl?: string): string {
  const trimmed = baseUrl?.trim().replace(/\/+$/, '')
  const base = trimmed || window.location.origin
  if (base.endsWith('/v1/chat/completions')) return base
  if (base.endsWith('/v1')) return `${base}/chat/completions`
  return `${base}/v1/chat/completions`
}

export function buildCurlCommand(args: {
  endpoint: string
  apiKey: string
  model: string
}): string {
  return [
    `curl ${args.endpoint} \\`,
    '  -H "Content-Type: application/json" \\',
    `  -H "Authorization: Bearer ${args.apiKey}" \\`,
    `  -d '{"model":"${args.model}","messages":[{"role":"user","content":"Say hello in one sentence."}]}'`,
  ].join('\n')
}

/** The address the gateway says it is called at, or this page's origin. */
export function useServerAddress(): string {
  const { status } = useStatus()
  const configured =
    typeof status?.server_address === 'string'
      ? status.server_address.trim()
      : ''
  return configured || window.location.origin
}

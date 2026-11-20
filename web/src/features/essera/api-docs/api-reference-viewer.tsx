import {
  ApiReferenceReact,
  type AnyApiReferenceConfiguration,
} from '@scalar/api-reference-react'

import '@scalar/api-reference-react/style.css'
import { useMemo } from 'react'

import { useTheme } from '@/context/theme-provider'

/**
 * Scalar's colours, type and radii taken from the app's theme tokens, which already follow the
 * light or dark theme, so the reference looks like the rest of the console in both.
 */
const THEME_CSS = `
.light-mode, .dark-mode, .light-mode .scalar-app, .dark-mode .scalar-app {
  --scalar-background-1: var(--background);
  --scalar-background-2: var(--card);
  --scalar-background-3: var(--muted);
  --scalar-background-accent: var(--accent);
  --scalar-color-1: var(--foreground);
  --scalar-color-2: var(--muted-foreground);
  --scalar-color-3: color-mix(in oklch, var(--muted-foreground) 80%, var(--background));
  --scalar-color-accent: var(--primary);
  --scalar-link-color: var(--primary);
  --scalar-border-color: var(--border);
  --scalar-focus-color: var(--ring);
  --scalar-button-1: var(--primary);
  --scalar-button-1-color: var(--primary-foreground);
  --scalar-button-1-hover: color-mix(in oklch, var(--primary) 88%, var(--foreground));
  --scalar-color-green: var(--success);
  --scalar-color-red: var(--destructive);
  --scalar-color-danger: var(--destructive);
  --scalar-color-yellow: var(--warning);
  --scalar-color-orange: var(--warning);
  --scalar-color-blue: var(--info);
  --scalar-font: var(--font-sans);
  --scalar-radius: var(--radius-sm);
  --scalar-radius-lg: var(--radius-md);
  --scalar-radius-xl: var(--radius-lg);
  --scalar-sidebar-background-1: var(--background);
  --scalar-sidebar-color-1: var(--foreground);
  --scalar-sidebar-color-2: var(--muted-foreground);
  --scalar-sidebar-color-active: var(--primary);
  --scalar-sidebar-item-hover-background: var(--accent);
  --scalar-sidebar-item-active-background: var(--accent);
  --scalar-sidebar-border-color: var(--border);
  --scalar-sidebar-search-background: var(--muted);
  --scalar-sidebar-search-border-color: var(--border);
  --scalar-sidebar-search-color: var(--muted-foreground);
}
.scalar-app .references-layout {
  min-height: 100%;
}
`

type ApiReferenceViewerProps = {
  document: Record<string, unknown>
}

/**
 * Scalar's API reference of the document. "Test request" calls the Gateway itself, the
 * document's server: no Scalar proxy, agent, MCP link, telemetry or remote fonts, and the key
 * a reader types in is not stored.
 */
export default function ApiReferenceViewer(props: ApiReferenceViewerProps) {
  const { resolvedTheme } = useTheme()
  const configuration = useMemo<AnyApiReferenceConfiguration>(
    () => ({
      content: props.document,
      theme: 'none',
      layout: 'modern',
      forceDarkModeState: resolvedTheme,
      hideDarkModeToggle: true,
      customCss: THEME_CSS,
      withDefaultFonts: false,
      hiddenClients: true,
      hideClientButton: true,
      documentDownloadType: 'json',
      persistAuth: false,
      telemetry: false,
      showDeveloperTools: 'never',
      showToolbar: 'never',
      agent: { disabled: true },
      mcp: { disabled: true },
      authentication: { preferredSecurityScheme: 'bearerAuth' },
      defaultOpenAllTags: true,
      hideModels: true,
    }),
    [props.document, resolvedTheme]
  )

  return (
    <div className='essera-api-reference -mx-3 -mb-3 sm:-mx-4 sm:-mb-4'>
      <ApiReferenceReact configuration={configuration} />
    </div>
  )
}

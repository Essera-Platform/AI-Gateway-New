import {
  ApiReferenceReact,
  type AnyApiReferenceConfiguration,
} from '@scalar/api-reference-react'

import '@scalar/api-reference-react/style.css'
import { useMemo } from 'react'

import { useTheme } from '@/context/theme-provider'

/**
 * Scalar's colours, type and radii taken from the app's theme tokens, which already follow the
 * light or dark theme, so the reference looks like the rest of the console in both. Request and
 * response examples sit on the designs' dark code surface in both themes, and the methods in the
 * sidebar are tinted chips.
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
  --scalar-font-code: var(--font-mono);
  --scalar-font-size-1: 22px;
  --scalar-font-size-2: 14px;
  --scalar-font-size-3: 13px;
  --scalar-paragraph: 14px;
  --scalar-small: 13px;
  --scalar-mini: 12px;
  --scalar-heading-1: 26px;
  --scalar-heading-2: 20px;
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
.scalar-app .markdown {
  font-size: var(--scalar-paragraph);
}
.scalar-app .sidebar-heading-type {
  padding: 1px 5px;
  border-radius: 4px;
  background: color-mix(in oklch, var(--method-color, currentColor) 12%, transparent);
}
.scalar-app .examples,
.scalar-app .request-card,
.scalar-app .response-card {
  --scalar-background-1: var(--code);
  --scalar-background-2: #111726;
  --scalar-background-3: #1e293b;
  --scalar-background-accent: rgb(16 185 129 / 0.12);
  --scalar-color-1: #f1f5f9;
  --scalar-color-2: #cbd5e1;
  --scalar-color-3: #94a3b8;
  --scalar-color-accent: #34d399;
  --scalar-border-color: #1e293b;
  --scalar-color-green: #34d399;
  --scalar-color-blue: #38bdf8;
  --scalar-color-orange: #fbbf24;
  --scalar-color-yellow: #fbbf24;
  --scalar-color-red: #fb7185;
  --scalar-color-purple: #a78bfa;
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
    <div className='essera-api-reference bg-card -mx-3 -mb-3 border-t sm:-mx-6 sm:-mb-6 lg:-mx-8'>
      <ApiReferenceReact configuration={configuration} />
    </div>
  )
}

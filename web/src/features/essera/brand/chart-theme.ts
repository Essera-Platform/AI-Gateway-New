import type { ITheme } from '@visactor/vchart/esm/theme/interface'

/** Series colours in the designs' order (emerald, sky, violet, amber, teal), then more hues that
 * read in Light and Dark. Rose is left out: it means an error. */
export const ESSERA_CHART_SERIES = [
  '#10b981',
  '#0ea5e9',
  '#8b5cf6',
  '#f59e0b',
  '#14b8a6',
  '#6366f1',
  '#84cc16',
  '#06b6d4',
  '#d946ef',
  '#64748b',
] as const

const SANS = '"Plus Jakarta Sans Variable", "Plus Jakarta Sans", system-ui, sans-serif'
const MONO = '"JetBrains Mono Variable", "JetBrains Mono", ui-monospace, monospace'

/** The designs' chart look: mono axis labels and legend, dashed grid lines. */
export function esseraChartTheme(mode: 'light' | 'dark'): ITheme {
  const dark = mode === 'dark'
  const grid = dark ? '#1e293b' : '#e2e8f0'
  const label = dark ? '#94a3b8' : '#64748b'
  const legend = dark ? '#cbd5e1' : '#475569'
  const monoLabel = { fontFamily: MONO, fontSize: 11, fill: label }

  return {
    type: mode,
    fontFamily: SANS,
    component: {
      axis: {
        label: { style: monoLabel },
        grid: { style: { stroke: grid, lineDash: [3, 3] } },
        domainLine: { style: { stroke: grid } },
      },
      discreteLegend: {
        item: {
          label: { style: { fontFamily: MONO, fontSize: 11, fill: legend } },
        },
      },
    },
  } as ITheme
}

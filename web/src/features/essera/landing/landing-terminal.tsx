import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

type Sample = {
  id: string
  label: string
  path: string
  headers: string[]
  body: string[]
  response: string[]
}

/** One request in each format a managed gateway serves; the figures are an example answer. */
const SAMPLES: Sample[] = [
  {
    id: 'chat',
    label: 'Chat',
    path: '/v1/chat/completions',
    headers: ['"Authorization: Bearer sk-•••••"'],
    body: [
      '"model": "your-model",',
      '"messages": [{ "role": "user", "content": "..." }]',
    ],
    response: [
      '"choices": [{ "message": { "content": "Request served." } }],',
      '"usage": { "total_tokens": 27 }',
    ],
  },
  {
    id: 'responses',
    label: 'Responses',
    path: '/v1/responses',
    headers: ['"Authorization: Bearer sk-•••••"'],
    body: ['"model": "your-model",', '"input": "..."'],
    response: [
      '"output": [{ "type": "output_text", "text": "Response ready." }],',
      '"usage": { "total_tokens": 31 }',
    ],
  },
  {
    id: 'messages',
    label: 'Messages',
    path: '/v1/messages',
    headers: ['"x-api-key: sk-•••••"', '"anthropic-version: 2023-06-01"'],
    body: [
      '"model": "your-model",',
      '"max_tokens": 1024,',
      '"messages": [{ "role": "user", "content": "..." }]',
    ],
    response: [
      '"content": [{ "type": "text", "text": "Message answered." }],',
      '"usage": { "input_tokens": 12, "output_tokens": 17 }',
    ],
  },
]

const TOKEN_RE = /("[^"]*")(\s*:)?|(\b\d+\b)/g

function JsonLine(props: { line: string }) {
  const parts: React.ReactNode[] = []
  let cursor = 0
  for (const match of props.line.matchAll(TOKEN_RE)) {
    const start = match.index ?? 0
    if (start > cursor) parts.push(props.line.slice(cursor, start))
    if (match[3]) {
      parts.push(
        <span key={start} className='text-emerald-400'>
          {match[3]}
        </span>
      )
    } else if (match[2]) {
      parts.push(
        <span key={start} className='text-violet-300'>
          {match[1]}
        </span>,
        match[2]
      )
    } else {
      parts.push(
        <span key={start} className='text-amber-200'>
          {match[1]}
        </span>
      )
    }
    cursor = start + match[0].length
  }
  parts.push(props.line.slice(cursor))
  return <div className='break-words whitespace-pre-wrap'>{parts}</div>
}

/** The landing's request card: a sample call and its answer in each API format. */
export function LandingTerminal() {
  const { t } = useTranslation()
  const [activeId, setActiveId] = useState('responses')
  const sample = SAMPLES.find((item) => item.id === activeId) ?? SAMPLES[0]

  return (
    <div className='bg-card overflow-hidden rounded-2xl border shadow-[var(--shadow-terminal)]'>
      <div className='bg-muted/40 flex items-center justify-between border-b px-4 pt-3'>
        <div
          role='tablist'
          aria-label={t('API format')}
          className='text-muted-foreground flex items-center gap-1 overflow-x-auto pb-2 text-xs font-medium sm:gap-3'
        >
          {SAMPLES.map((item) => (
            <button
              key={item.id}
              type='button'
              role='tab'
              aria-selected={item.id === sample.id}
              onClick={() => setActiveId(item.id)}
              className={cn(
                'px-2.5 py-1 transition-colors',
                item.id === sample.id
                  ? 'border-primary text-primary -mb-[9px] border-b-2 pb-[7px] font-semibold'
                  : 'hover:text-foreground'
              )}
            >
              {item.label}
            </button>
          ))}
        </div>
        <div className='text-primary flex items-center gap-1.5 pb-2 font-mono text-xs font-medium'>
          <span
            aria-hidden='true'
            className='size-2 rounded-full bg-emerald-500'
          />
          200 OK
        </div>
      </div>
      <div className='flex items-center gap-2 border-b px-5 py-3 font-mono text-xs'>
        <span className='rounded border border-amber-200/60 bg-amber-50 px-2 py-0.5 font-bold text-amber-700 uppercase dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-300'>
          POST
        </span>
        <span className='text-foreground font-medium'>{sample.path}</span>
      </div>
      <div className='bg-code min-h-[170px] overflow-x-auto p-5 font-mono text-xs leading-relaxed text-slate-200'>
        <div className='mb-2 text-[10px] font-semibold tracking-wider text-slate-400 uppercase'>
          {t('Request')}
        </div>
        <div>
          <span className='text-emerald-400'>curl</span>{' '}
          <span className='text-sky-300'>-X POST</span>{' '}
          <span className='text-amber-300'>&quot;{sample.path}&quot;</span> \
        </div>
        {sample.headers.map((header) => (
          <div key={header} className='pl-4'>
            <span className='text-sky-300'>-H</span>{' '}
            <span className='text-amber-300'>{header}</span> \
          </div>
        ))}
        <div className='pl-4'>
          <span className='text-sky-300'>-d</span> &apos;{'{'}
        </div>
        {sample.body.map((line) => (
          <div key={line} className='pl-8'>
            <JsonLine line={line} />
          </div>
        ))}
        <div className='pl-4'>{'}'}&apos;</div>
      </div>
      <div className='min-h-[118px] overflow-x-auto border-t border-slate-800 bg-slate-900 p-5 font-mono text-xs leading-relaxed text-slate-300'>
        <div className='mb-2 text-[10px] font-semibold tracking-wider text-slate-400 uppercase'>
          {t('Response')}
        </div>
        <div>{'{'}</div>
        {sample.response.map((line) => (
          <div key={line} className='pl-4'>
            <JsonLine line={line} />
          </div>
        ))}
        <div>{'}'}</div>
      </div>
      <div className='bg-code flex flex-wrap items-center justify-between gap-2 border-t border-slate-800 px-5 py-2.5 font-mono text-[11px] text-slate-400'>
        <span className='text-emerald-400'>
          {sample.id === 'messages'
            ? t('Anthropic-compatible')
            : t('OpenAI-compatible')}
        </span>
        <span className='text-[10px] font-semibold tracking-wider'>
          STREAM • SSE
        </span>
      </div>
    </div>
  )
}

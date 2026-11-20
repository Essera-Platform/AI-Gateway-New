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
// Modified by Essera, 2026-11: the design's empty state: a glowing icon tile and starter cards with a line each.
import {
  BarChartIcon,
  CodeSquareIcon,
  GraduationCapIcon,
  SquareTerminalIcon,
  NotepadTextIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

type PlaygroundEmptyStateProps = {
  onSelectPrompt: (prompt: string) => void
}

const starterPrompts = [
  {
    icon: BarChartIcon,
    text: 'Analyze data',
    description: 'Ask the model to read figures and explain the trends.',
  },
  {
    icon: NotepadTextIcon,
    text: 'Summarize text',
    description: 'Turn a long passage into a short summary.',
  },
  {
    icon: CodeSquareIcon,
    text: 'Code',
    description: 'Write, explain or fix a piece of code.',
  },
  {
    icon: GraduationCapIcon,
    text: 'Get advice',
    description: 'Talk a question or a decision through.',
  },
]

export function PlaygroundEmptyState({
  onSelectPrompt,
}: PlaygroundEmptyStateProps) {
  const { t } = useTranslation()

  return (
    <div className='relative flex min-h-[min(480px,calc(100svh-24rem))] items-center justify-center px-1 py-4 md:py-6'>
      <div
        aria-hidden='true'
        className='pointer-events-none absolute inset-x-0 top-0 h-64 bg-[radial-gradient(ellipse_at_top,color-mix(in_oklch,var(--primary)_10%,transparent),transparent_70%)]'
      />
      <div className='relative grid w-full max-w-3xl gap-5 text-center'>
        <div className='border-primary/25 bg-primary/10 text-primary mx-auto flex size-12 items-center justify-center rounded-2xl border shadow-[0_0_40px_-8px_color-mix(in_oklch,var(--primary)_45%,transparent)]'>
          <SquareTerminalIcon className='size-6' aria-hidden='true' />
        </div>

        <div className='grid gap-2'>
          <h2 className='text-xl font-bold tracking-tight text-balance md:text-2xl'>
            {t('Start a playground chat')}
          </h2>
          <p className='text-muted-foreground mx-auto max-w-lg text-sm leading-6 text-balance'>
            {t(
              'Test a model with a starter prompt, or write your own request below.'
            )}
          </p>
        </div>

        <div className='grid gap-3 text-left sm:grid-cols-2'>
          {starterPrompts.map(({ icon: Icon, text, description }) => {
            const prompt = t(text)

            return (
              <Button
                className='bg-card hover:bg-card hover:border-primary/40 h-auto items-start justify-start gap-3 rounded-xl p-3.5 text-left font-normal whitespace-normal shadow-xs hover:shadow-[0_8px_24px_-4px_color-mix(in_oklch,var(--primary)_10%,transparent)]'
                key={text}
                onClick={() => onSelectPrompt(prompt)}
                variant='outline'
              >
                <span className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg'>
                  <Icon className='size-4' aria-hidden='true' />
                </span>
                <span className='grid min-w-0 gap-0.5'>
                  <span className='text-foreground text-sm font-semibold'>
                    {prompt}
                  </span>
                  <span className='text-muted-foreground text-[13px] leading-5'>
                    {t(description)}
                  </span>
                </span>
              </Button>
            )
          })}
        </div>
      </div>
    </div>
  )
}

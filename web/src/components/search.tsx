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
// Modified by Essera, 2026-11: the designs' 30px search field with 12px text and a white ⌘ K chip.
import { SearchIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { useSearch } from '@/context/search-provider'
import { cn } from '@/lib/utils'

import { Button } from './ui/button'

type SearchProps = {
  className?: string
  type?: React.HTMLInputTypeAttribute
  placeholder?: string
}

export function Search({ className = '', placeholder }: SearchProps) {
  const { t } = useTranslation()
  const { setOpen } = useSearch()
  const resolvedPlaceholder = placeholder ?? t('Search')
  return (
    <Button
      variant='outline'
      className={cn(
        'bg-muted/70 group text-muted-foreground hover:bg-accent relative h-[30px] w-full flex-1 justify-start rounded-lg text-xs font-normal shadow-none sm:w-40 sm:pe-12 md:flex-none lg:w-52 xl:w-64',
        className
      )}
      onClick={() => setOpen(true)}
      aria-label={resolvedPlaceholder}
    >
      <SearchIcon
        aria-hidden='true'
        className='text-muted-foreground/70 absolute start-2.5 top-1/2 -translate-y-1/2'
        size={14}
      />
      <span className='ms-5'>{resolvedPlaceholder}</span>
      <kbd className='bg-card text-muted-foreground/70 pointer-events-none absolute end-2 top-1/2 hidden h-[18px] -translate-y-1/2 items-center gap-1 rounded border px-1.5 font-mono text-[10px] font-semibold opacity-100 shadow-2xs select-none sm:flex'>
        <span>⌘</span>
        {t('K')}
      </kbd>
    </Button>
  )
}

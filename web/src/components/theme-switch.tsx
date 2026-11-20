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
// Modified by Essera, 2026-11: one button that switches between Light and Dark; there is no System choice.
import { Moon, Sun } from 'lucide-react'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { useTheme } from '@/context/theme-provider'
import { cn } from '@/lib/utils'

export function ThemeSwitch(props: { className?: string }) {
  const { t } = useTranslation()
  const { theme, setTheme } = useTheme()
  const dark = theme === 'dark'

  /* Update theme-color meta tag
   * when theme is updated */
  useEffect(() => {
    const themeColor = dark ? '#0b0f19' : '#ffffff'
    const metaThemeColor = document.querySelector("meta[name='theme-color']")
    if (metaThemeColor) metaThemeColor.setAttribute('content', themeColor)
  }, [dark])

  const label = dark ? t('Switch to light theme') : t('Switch to dark theme')

  return (
    <Button
      variant='ghost'
      size='icon'
      className={cn(
        'text-muted-foreground hover:text-foreground size-8',
        props.className
      )}
      onClick={() => setTheme(dark ? 'light' : 'dark')}
      aria-label={label}
      title={label}
    >
      {dark ? <Sun className='size-4' /> : <Moon className='size-4' />}
    </Button>
  )
}

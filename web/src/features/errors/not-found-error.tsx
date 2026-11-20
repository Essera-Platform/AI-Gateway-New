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
// Modified by Essera, 2026-11: the designs' error page (ErrorPage), with Overview and the Model Square as ways out.
import { useTranslation } from 'react-i18next'

import { ErrorPage } from '@/features/essera/error-page'

export function NotFoundError() {
  const { t } = useTranslation()
  return (
    <ErrorPage
      status={404}
      title={t('Page not found')}
      description={t(
        'The page you are looking for does not exist, was removed, or has moved to another address.'
      )}
    />
  )
}

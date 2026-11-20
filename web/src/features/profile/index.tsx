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
// Modified by Essera, 2026-11: a managed gateway's people have no notification or unpriced-model settings.
// Modified by Essera, 2026-11: with no settings column left, the sidebar settings take the full width; the designs' page padding.
import { Main } from '@/components/layout'
import {
  CardStaggerContainer,
  CardStaggerItem,
} from '@/components/page-transition'
import {
  useEsseraManaged,
  useEsseraPerson,
} from '@/features/essera/use-essera-managed'
import { useStatus } from '@/hooks/use-status'
import { useAuthStore } from '@/stores/auth-store'

import { CheckinCalendarCard } from './components/checkin-calendar-card'
import { LanguagePreferencesCard } from './components/language-preferences-card'
import { ProfileHeader } from './components/profile-header'
import { ProfileSettingsCard } from './components/profile-settings-card'
import { SidebarModulesCard } from './components/sidebar-modules-card'
import { useProfile } from './hooks'

export function Profile() {
  const { profile, loading, refreshProfile } = useProfile()
  const { status } = useStatus()
  const permissions = useAuthStore((s) => s.auth.user?.permissions)
  const esseraPerson = useEsseraPerson()
  const { managed } = useEsseraManaged()

  const checkinEnabled = status?.checkin_enabled === true
  const turnstileEnabled = !!(
    status?.turnstile_check && status?.turnstile_site_key
  )
  const turnstileSiteKey = status?.turnstile_site_key || ''
  const canConfigureSidebar = permissions?.sidebar_settings !== false
  // Older cached status payloads lack these fields; the site default is on.
  const quotaRemindEnabled = status?.quota_remind_enabled !== false
  const quotaRemindThreshold = status?.quota_remind_threshold ?? 0
  const showSettingsColumn = !esseraPerson || !managed

  return (
    <Main>
      <div className='min-h-0 flex-1 overflow-auto px-3 py-3 sm:px-6 sm:py-6 lg:px-8'>
        <CardStaggerContainer className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-6'>
          <CardStaggerItem>
            <ProfileHeader profile={profile} loading={loading} />
          </CardStaggerItem>

          {!showSettingsColumn && (
            <CardStaggerItem className='space-y-4 sm:space-y-6'>
              {checkinEnabled && (
                <CheckinCalendarCard
                  checkinEnabled={checkinEnabled}
                  turnstileEnabled={turnstileEnabled}
                  turnstileSiteKey={turnstileSiteKey}
                />
              )}
              {canConfigureSidebar && <SidebarModulesCard wide />}
            </CardStaggerItem>
          )}

          {showSettingsColumn && (
          <CardStaggerItem>
            <div className='grid gap-4 sm:gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(360px,0.46fr)] xl:items-start'>
              <div className='space-y-4 sm:space-y-6'>
                {!esseraPerson && (
                  <ProfileSettingsCard
                    profile={profile}
                    loading={loading}
                    onProfileUpdate={refreshProfile}
                    quotaRemindEnabled={quotaRemindEnabled}
                    quotaRemindThreshold={quotaRemindThreshold}
                  />
                )}
                {/* Modified by Essera, 2026-11: a managed gateway is English only. */}
                {!managed && (
                  <LanguagePreferencesCard
                    profile={profile}
                    onProfileUpdate={refreshProfile}
                  />
                )}
              </div>

              <div className='space-y-4 sm:space-y-6 xl:sticky xl:top-6'>
                {checkinEnabled && (
                  <CheckinCalendarCard
                    checkinEnabled={checkinEnabled}
                    turnstileEnabled={turnstileEnabled}
                    turnstileSiteKey={turnstileSiteKey}
                  />
                )}
                {canConfigureSidebar && <SidebarModulesCard />}
              </div>
            </div>
          </CardStaggerItem>
          )}
        </CardStaggerContainer>
      </div>
    </Main>
  )
}

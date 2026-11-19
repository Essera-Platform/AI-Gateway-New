import type { TFunction } from 'i18next'

// Rows the gateway reads from the Console's audit carry op.params.source "console".
export function isConsoleAuditEvent(params: Record<string, unknown>): boolean {
  return params.source === 'console'
}

export function consoleAuditActionLabel(
  action: string,
  newStatus: unknown,
  t: TFunction
): string {
  switch (action) {
    case 'credential.create':
      return t('API key created')
    case 'credential.revoke':
      return t('API key revoked')
    case 'credential.rotate':
      return t('API key rotated')
    case 'credential.expire':
      return t('API key expiry changed')
    case 'end_user.create':
      return t('User added')
    case 'end_user.update':
      if (newStatus === 'suspended') return t('User suspended')
      if (newStatus === 'active') return t('User reactivated')
      if (newStatus === 'closed') return t('User removed')
      return t('User changed')
    case 'end_user.bulk_upload':
      return t('Users uploaded')
    case 'gateway_access.grant':
      return t('Sign-in given')
    case 'gateway_access.email':
      return t('Sign-in email changed')
    case 'gateway_access.remove':
      return t('Sign-in removed')
    case 'gateway_access.set_up':
      return t('Sign-in set up')
    case 'gateway_access.invite':
      return t('Invitation sent again')
    case 'account.create':
      return t('Account created')
    case 'account.profile':
      return t('Account details changed')
    case 'account.entitlements':
      return t('Account limits changed')
    case 'account.status':
      return t('Account status changed')
    default:
      return action
  }
}

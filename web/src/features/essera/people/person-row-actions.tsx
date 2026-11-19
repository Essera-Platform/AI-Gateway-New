import { Power, PowerOff, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableRowActionMenu } from '@/components/data-table/core/row-action-menu'
import {
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
} from '@/components/ui/dropdown-menu'
import { handleServerError } from '@/lib/handle-server-error'

import {
  canChangePerson,
  changePerson,
  type EsseraPerson,
  type EsseraPersonAction,
} from './api'
import { peopleMessage } from './messages'

const DONE: Record<EsseraPersonAction, string> = {
  suspend: 'User disabled successfully',
  reactivate: 'User enabled successfully',
  remove: 'User deleted successfully',
}

type PersonRowActionsProps = {
  person: EsseraPerson
  onChanged: (person: EsseraPerson, action: EsseraPersonAction) => void
}

/** new-api's user row actions, Enable, Disable and Delete, carried out at the Console. */
export function PersonRowActions(props: PersonRowActionsProps) {
  const { t } = useTranslation()
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [busy, setBusy] = useState(false)
  const person = props.person

  if (!canChangePerson(person)) return null

  const run = async (action: EsseraPersonAction) => {
    setBusy(true)
    try {
      const result = await changePerson(person.id, action)
      if (!result.success) {
        toast.error(peopleMessage(t, result))
        return
      }
      toast.success(t(DONE[action]))
      const changed =
        result.data && 'status' in result.data ? result.data : person
      props.onChanged({ ...person, ...changed, pending: true }, action)
    } catch (error) {
      handleServerError(error, t('Data is temporarily unavailable'))
    } finally {
      setBusy(false)
      setConfirmDelete(false)
    }
  }

  const suspended = person.status === 'suspended'

  return (
    <div className='-ml-1.5 flex items-center gap-1'>
      <DataTableRowActionMenu
        ariaLabel={t('Open menu')}
        contentClassName='w-48'
      >
        {suspended ? (
          <DropdownMenuItem disabled={busy} onClick={() => run('reactivate')}>
            {t('Enable')}
            <DropdownMenuShortcut>
              <Power size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>
        ) : (
          <DropdownMenuItem disabled={busy} onClick={() => run('suspend')}>
            {t('Disable')}
            <DropdownMenuShortcut>
              <PowerOff size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          disabled={busy}
          onClick={() => setConfirmDelete(true)}
          className='text-destructive focus:text-destructive'
        >
          {t('Delete')}
          <DropdownMenuShortcut>
            <Trash2 size={16} />
          </DropdownMenuShortcut>
        </DropdownMenuItem>
      </DataTableRowActionMenu>

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={t('Are you sure?')}
        desc={
          <>
            {t('This will permanently delete user')}{' '}
            <span className='font-semibold'>{person.email}</span>
            {t('. This action cannot be undone.')}
          </>
        }
        confirmText={busy ? t('Deleting...') : t('Delete')}
        destructive
        isLoading={busy}
        handleConfirm={() => run('remove')}
      />
    </div>
  )
}

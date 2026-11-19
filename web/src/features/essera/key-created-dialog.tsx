import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'

import type { EsseraCreatedKey } from './api'

type KeyCreatedDialogProps = {
  created: EsseraCreatedKey | null
  onClose: () => void
}

export function KeyCreatedDialog(props: KeyCreatedDialogProps) {
  const { t } = useTranslation()

  return (
    <Dialog
      open={props.created !== null}
      onOpenChange={(open) => !open && props.onClose()}
      title={t('API key created')}
      description={t('Copy this key now. It will not be shown again.')}
      contentClassName='sm:max-w-lg'
      footer={
        <Button type='button' onClick={props.onClose}>
          {t('Done')}
        </Button>
      }
    >
      {props.created && (
        <div className='grid gap-2'>
          <p className='text-sm font-medium'>{props.created.name}</p>
          <div className='flex items-center gap-2'>
            <code
              aria-label={t('API Key')}
              className='bg-muted/50 min-w-0 flex-1 rounded-md border px-3 py-2 font-mono text-xs break-all select-all'
            >
              {props.created.key}
            </code>
            <CopyButton
              value={props.created.key}
              variant='outline'
              tooltip={t('Copy API key')}
            />
          </div>
        </div>
      )}
    </Dialog>
  )
}

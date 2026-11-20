import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'

import type { EsseraCreatedKey } from './api'
import {
  FALLBACK_EXAMPLE_MODEL,
  buildCurlCommand,
  chatCompletionsEndpoint,
  useServerAddress,
  userModelsQueryOptions,
} from './example-request'

type KeyCreatedDialogProps = {
  created: EsseraCreatedKey | null
  onClose: () => void
}

export function KeyCreatedDialog(props: KeyCreatedDialogProps) {
  const { t } = useTranslation()
  const serverAddress = useServerAddress()
  const modelsQuery = useQuery({
    ...userModelsQueryOptions,
    enabled: props.created !== null,
  })
  const exampleRequest = props.created
    ? buildCurlCommand({
        endpoint: chatCompletionsEndpoint(serverAddress),
        apiKey: props.created.key,
        model: modelsQuery.data?.[0] ?? FALLBACK_EXAMPLE_MODEL,
      })
    : ''

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
          <CopyButton
            value={exampleRequest}
            variant='outline'
            size='sm'
            className='justify-self-start'
            aria-label={t('Copy example request')}
          >
            {t('Copy example request')}
          </CopyButton>
        </div>
      )}
    </Dialog>
  )
}

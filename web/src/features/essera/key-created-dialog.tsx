import { useQuery } from '@tanstack/react-query'
import { ShieldCheck, TriangleAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { formatDateTimeObject } from '@/lib/time'
import { cn } from '@/lib/utils'

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

const fieldLabelClassName =
  'text-muted-foreground mb-1.5 text-[11px] font-bold tracking-wider uppercase'

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
  const expiresAt = props.created?.expires_at
    ? new Date(props.created.expires_at)
    : null

  return (
    <Dialog
      open={props.created !== null}
      onOpenChange={(open) => !open && props.onClose()}
      title={
        <span className='flex items-center gap-3'>
          <span className='bg-primary/10 text-primary border-primary/20 flex size-10 shrink-0 items-center justify-center rounded-xl border'>
            <ShieldCheck className='size-5' aria-hidden='true' />
          </span>
          <span className='text-xl font-bold tracking-tight'>
            {t('API key created')}
          </span>
        </span>
      }
      description={t('Copy this key now. It will not be shown again.')}
      descriptionClassName='ps-[52px]'
      contentClassName='overflow-hidden rounded-2xl border-t-4 border-t-primary sm:max-w-xl'
      footerClassName='bg-muted/40 border-t'
      footer={
        <Button type='button' onClick={props.onClose}>
          {t('Done')}
        </Button>
      }
    >
      {props.created && (
        <div className='grid gap-4'>
          <div className='border-warning/30 bg-warning/10 text-warning flex items-start gap-2.5 rounded-lg border px-3.5 py-2.5 text-[13px] font-medium'>
            <TriangleAlert
              className='mt-0.5 size-4 shrink-0'
              aria-hidden='true'
            />
            <span>
              {t(
                'Store it in a secret manager. If you lose it, delete this key and create a new one.'
              )}
            </span>
          </div>

          <div>
            <p className={fieldLabelClassName}>{t('Name')}</p>
            <p className='bg-muted inline-block rounded-md px-3 py-1.5 font-mono text-sm font-semibold'>
              {props.created.name}
            </p>
          </div>

          <div>
            <p className={fieldLabelClassName}>{t('Your API key')}</p>
            <div className='bg-muted/60 flex items-stretch overflow-hidden rounded-lg border'>
              <code
                aria-label={t('API Key')}
                className='min-w-0 flex-1 self-center px-3 py-2.5 font-mono text-[13px] break-all select-all'
              >
                {props.created.key}
              </code>
              <CopyButton
                value={props.created.key}
                variant='default'
                size='default'
                className='h-auto rounded-none px-4'
                aria-label={t('Copy API key')}
              >
                {t('Copy key')}
              </CopyButton>
            </div>
            {expiresAt && !Number.isNaN(expiresAt.getTime()) && (
              <p className='text-muted-foreground mt-2 flex items-center gap-1.5 font-mono text-[11px]'>
                <span
                  className='bg-primary size-1.5 rounded-full'
                  aria-hidden='true'
                />
                {t('Expires')} {formatDateTimeObject(expiresAt)}
              </p>
            )}
          </div>

          <div>
            <div className='mb-1.5 flex items-center justify-between gap-2'>
              <p className={cn(fieldLabelClassName, 'mb-0')}>
                {t('Example request')}
              </p>
              <CopyButton
                value={exampleRequest}
                variant='outline'
                size='sm'
                aria-label={t('Copy example request')}
              >
                {t('Copy example request')}
              </CopyButton>
            </div>
            <pre className='bg-code max-h-48 overflow-auto rounded-xl p-4 font-mono text-xs leading-relaxed whitespace-pre-wrap text-slate-200'>
              {exampleRequest}
            </pre>
          </div>
        </div>
      )}
    </Dialog>
  )
}

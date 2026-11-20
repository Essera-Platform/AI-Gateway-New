import { FileTextIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  PromptInputAttachment,
  PromptInputAttachments,
  PromptInputHeader,
  usePromptInputAttachments,
} from '@/components/ai-elements/prompt-input'
import { cn } from '@/lib/utils'

import type { MessageAttachment } from './attachments'

/** The files attached to the message being written, each removable. */
export function PendingAttachments() {
  const attachments = usePromptInputAttachments()
  if (attachments.files.length === 0) return null

  return (
    <PromptInputHeader className='px-4 pt-3'>
      <PromptInputAttachments>
        {(file) => <PromptInputAttachment data={file} />}
      </PromptInputAttachments>
    </PromptInputHeader>
  )
}

type MessageAttachmentsProps = {
  attachments?: MessageAttachment[]
}

/** The files a sent message carried. */
export function MessageAttachments(props: MessageAttachmentsProps) {
  const { t } = useTranslation()
  if (!props.attachments?.length) return null

  return (
    <ul aria-label={t('Attachments')} className='mb-1.5 flex flex-wrap gap-1.5'>
      {props.attachments.map((attachment) => (
        <li
          className={cn(
            'border-border flex h-8 max-w-60 items-center gap-1.5 rounded-md border px-1.5 text-sm font-medium',
            !attachment.url && 'text-muted-foreground'
          )}
          key={attachment.id}
          title={attachment.url ? attachment.name : t('Not kept after reload')}
        >
          {attachment.kind === 'image' && attachment.url ? (
            <img
              alt=''
              className='size-5 shrink-0 rounded object-cover'
              height={20}
              src={attachment.url}
              width={20}
            />
          ) : (
            <FileTextIcon
              aria-hidden='true'
              className='text-muted-foreground size-4 shrink-0'
            />
          )}
          <span className='truncate'>{attachment.name}</span>
          {!attachment.url && (
            <span className='sr-only'>{t('Not kept after reload')}</span>
          )}
        </li>
      ))}
    </ul>
  )
}

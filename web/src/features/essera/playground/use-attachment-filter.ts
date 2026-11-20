import type { FileUIPart } from 'ai'
import { useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { vetAttachmentFiles, type AttachmentRejection } from './attachments'

/**
 * PromptInput's filterFiles for the playground: keeps the files that fit the
 * message's limits and says why each other one was left out.
 */
export function useAttachmentFilter() {
  const { t } = useTranslation()

  return useCallback(
    (incoming: File[], current: FileUIPart[]): File[] => {
      const { accepted, rejected } = vetAttachmentFiles(incoming, current)
      for (const rejection of rejected) {
        toast.error(describeRejection(rejection, t))
      }
      return accepted
    },
    [t]
  )
}

function describeRejection(
  rejection: AttachmentRejection,
  t: (key: string, options?: Record<string, unknown>) => string
): string {
  switch (rejection.reason) {
    case 'type':
      return t('{{name}} is not a supported file type', {
        name: rejection.name,
      })
    case 'size':
      return t('{{name}} is larger than {{size}} MB', {
        name: rejection.name,
        size: rejection.limit / (1024 * 1024),
      })
    case 'count':
      return t('A message can carry up to {{limit}} files', {
        limit: rejection.limit,
      })
    case 'documents':
      return t('A message can carry up to {{limit}} documents', {
        limit: rejection.limit,
      })
  }
}

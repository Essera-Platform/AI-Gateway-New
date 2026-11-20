import {
  CameraIcon,
  FileIcon,
  ImageIcon,
  PaperclipIcon,
  ScreenShareIcon,
} from 'lucide-react'
import { useRef, useState, type ChangeEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  PromptInputButton,
  usePromptInputAttachments,
} from '@/components/ai-elements/prompt-input'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'

import { FILE_ACCEPT, IMAGE_ACCEPT } from './attachments'
import { CameraDialog } from './camera-dialog'
import {
  canCaptureScreen,
  canUseCamera,
  captureScreenshot,
} from './media-capture'

type AttachMenuProps = {
  disabled?: boolean
}

/**
 * The playground's Attach menu: upload a file or a photo, take a screenshot
 * or a photo. Screenshot and photo are offered only where the browser can
 * take them.
 */
export function AttachMenu(props: AttachMenuProps) {
  const { t } = useTranslation()
  const attachments = usePromptInputAttachments()
  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const photoInputRef = useRef<HTMLInputElement | null>(null)
  const [cameraOpen, setCameraOpen] = useState(false)

  const handleFiles = (event: ChangeEvent<HTMLInputElement>) => {
    const files = [...(event.currentTarget.files ?? [])]
    event.currentTarget.value = ''
    if (files.length > 0) attachments.add(files)
  }

  const handleScreenshot = async () => {
    try {
      attachments.add([await captureScreenshot()])
    } catch (error) {
      if (error instanceof DOMException && error.name === 'NotAllowedError') {
        return
      }
      toast.error(t('Could not take a screenshot'))
    }
  }

  return (
    <>
      <input
        accept={FILE_ACCEPT}
        aria-label={t('Upload file')}
        className='hidden'
        multiple
        onChange={handleFiles}
        ref={fileInputRef}
        type='file'
      />
      <input
        accept={IMAGE_ACCEPT}
        aria-label={t('Upload photo')}
        className='hidden'
        multiple
        onChange={handleFiles}
        ref={photoInputRef}
        type='file'
      />
      <Tooltip>
        <DropdownMenu>
          <TooltipTrigger
            render={
              <DropdownMenuTrigger
                render={
                  <PromptInputButton
                    aria-label={t('Attach')}
                    className='text-muted-foreground hover:text-foreground hover:bg-muted/70 font-medium'
                    disabled={props.disabled}
                    variant='ghost'
                  />
                }
              >
                <PaperclipIcon size={16} />
              </DropdownMenuTrigger>
            }
          />
          <TooltipContent>
            <p>{t('Attach')}</p>
          </TooltipContent>
          <DropdownMenuContent align='start'>
            <DropdownMenuItem onClick={() => fileInputRef.current?.click()}>
              <FileIcon className='mr-2' size={16} />
              {t('Upload file')}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => photoInputRef.current?.click()}>
              <ImageIcon className='mr-2' size={16} />
              {t('Upload photo')}
            </DropdownMenuItem>
            {canCaptureScreen() && (
              <DropdownMenuItem onClick={handleScreenshot}>
                <ScreenShareIcon className='mr-2' size={16} />
                {t('Take screenshot')}
              </DropdownMenuItem>
            )}
            {canUseCamera() && (
              <DropdownMenuItem onClick={() => setCameraOpen(true)}>
                <CameraIcon className='mr-2' size={16} />
                {t('Take photo')}
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </Tooltip>
      <CameraDialog
        open={cameraOpen}
        onOpenChange={setCameraOpen}
        onCapture={(file) => attachments.add([file])}
      />
    </>
  )
}

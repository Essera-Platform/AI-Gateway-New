import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'

import { captureVideoFrame, stopStream } from './media-capture'

type CameraDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCapture: (file: File) => void
}

export function CameraDialog(props: CameraDialogProps) {
  const { t } = useTranslation()
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const [ready, setReady] = useState(false)
  const { open, onOpenChange } = props

  useEffect(() => {
    if (!open) return
    let stream: MediaStream | null = null
    let cancelled = false

    navigator.mediaDevices
      .getUserMedia({ video: true, audio: false })
      .then((media) => {
        if (cancelled) {
          stopStream(media)
          return
        }
        stream = media
        if (videoRef.current) videoRef.current.srcObject = media
      })
      .catch(() => {
        if (cancelled) return
        toast.error(t('Could not open the camera'))
        onOpenChange(false)
      })

    return () => {
      cancelled = true
      stopStream(stream)
      setReady(false)
    }
  }, [open, onOpenChange, t])

  const handleCapture = async () => {
    if (!videoRef.current) return
    try {
      props.onCapture(
        await captureVideoFrame(videoRef.current, `photo-${Date.now()}`)
      )
      onOpenChange(false)
    } catch {
      toast.error(t('Could not take a photo'))
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Take photo')}
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='button' disabled={!ready} onClick={handleCapture}>
            {t('Capture')}
          </Button>
        </>
      }
    >
      <video
        aria-label={t('Camera preview')}
        autoPlay
        className='bg-muted aspect-video w-full rounded-md object-contain'
        muted
        onLoadedData={() => setReady(true)}
        playsInline
        ref={videoRef}
      />
    </Dialog>
  )
}

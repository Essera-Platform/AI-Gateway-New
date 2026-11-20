import { ATTACHMENT_LIMITS } from './attachments'

export function canCaptureScreen(): boolean {
  return typeof navigator.mediaDevices?.getDisplayMedia === 'function'
}

export function canUseCamera(): boolean {
  return typeof navigator.mediaDevices?.getUserMedia === 'function'
}

export function stopStream(stream: MediaStream | null): void {
  for (const track of stream?.getTracks() ?? []) track.stop()
}

function canvasToBlob(
  canvas: HTMLCanvasElement,
  type: string,
  quality?: number
): Promise<Blob | null> {
  return new Promise((resolve) => canvas.toBlob(resolve, type, quality))
}

/**
 * The video's current frame as an image file: PNG when it fits Bedrock's
 * image limit, JPEG otherwise.
 */
export async function captureVideoFrame(
  video: HTMLVideoElement,
  baseName: string
): Promise<File> {
  const canvas = document.createElement('canvas')
  canvas.width = video.videoWidth
  canvas.height = video.videoHeight
  canvas.getContext('2d')?.drawImage(video, 0, 0)

  const png = await canvasToBlob(canvas, 'image/png')
  if (png && png.size <= ATTACHMENT_LIMITS.imageBytes) {
    return new File([png], `${baseName}.png`, { type: 'image/png' })
  }
  const jpeg = await canvasToBlob(canvas, 'image/jpeg', 0.85)
  if (!jpeg) throw new Error('The frame could not be encoded')
  return new File([jpeg], `${baseName}.jpg`, { type: 'image/jpeg' })
}

/** Asks the user for a screen, window or tab and captures it once. */
export async function captureScreenshot(): Promise<File> {
  const stream = await navigator.mediaDevices.getDisplayMedia({
    video: true,
    audio: false,
  })
  try {
    const video = document.createElement('video')
    video.muted = true
    video.srcObject = stream
    await video.play()
    await new Promise((resolve) => requestAnimationFrame(resolve))
    return await captureVideoFrame(video, `screenshot-${Date.now()}`)
  } finally {
    stopStream(stream)
  }
}

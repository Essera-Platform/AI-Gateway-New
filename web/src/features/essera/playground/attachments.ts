import type { FileUIPart } from 'ai'
import { nanoid } from 'nanoid'

import type { ContentPart } from '@/features/playground/types'

export type AttachmentKind = 'image' | 'document' | 'text'

export type MessageAttachment = {
  id: string
  kind: AttachmentKind
  name: string
  mediaType: string
  // A data URL. Saved conversations keep only the name, so after a reload
  // the attachment is no longer sent.
  url?: string
}

export type AttachmentRejection = {
  name: string
  reason: 'type' | 'size' | 'count' | 'documents'
  limit: number
}

const MB = 1024 * 1024

// Bedrock takes images of up to 3.75 MB and five documents of up to 4.5 MB.
export const ATTACHMENT_LIMITS = {
  files: 10,
  documents: 5,
  imageBytes: 3.75 * MB,
  documentBytes: 4.5 * MB,
  textBytes: 1 * MB,
} as const

const IMAGE_TYPES: Record<string, string> = {
  png: 'image/png',
  jpg: 'image/jpeg',
  jpeg: 'image/jpeg',
  gif: 'image/gif',
  webp: 'image/webp',
}

const DOCUMENT_TYPES: Record<string, string> = {
  pdf: 'application/pdf',
  doc: 'application/msword',
  docx: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  xls: 'application/vnd.ms-excel',
  xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
}

const TEXT_EXTENSIONS = new Set([
  'txt',
  'md',
  'markdown',
  'csv',
  'tsv',
  'json',
  'jsonl',
  'xml',
  'yaml',
  'yml',
  'toml',
  'ini',
  'log',
  'html',
  'htm',
  'css',
  'scss',
  'js',
  'jsx',
  'mjs',
  'ts',
  'tsx',
  'py',
  'go',
  'java',
  'kt',
  'c',
  'h',
  'cpp',
  'hpp',
  'cs',
  'rs',
  'rb',
  'php',
  'swift',
  'sh',
  'sql',
])

const TEXT_MEDIA_TYPES = new Set([
  'application/json',
  'application/xml',
  'application/x-yaml',
  'application/yaml',
  'application/javascript',
  'application/sql',
  'application/x-sh',
])

export const IMAGE_ACCEPT = [...new Set(Object.values(IMAGE_TYPES))].join(',')

export const FILE_ACCEPT = [
  ...Object.keys(IMAGE_TYPES),
  ...Object.keys(DOCUMENT_TYPES),
  ...TEXT_EXTENSIONS,
]
  .map((extension) => `.${extension}`)
  .join(',')

/**
 * What an attached file is sent as, and its media type, judged by the file
 * name first: browsers often leave the type empty or wrong (".ts" is a video
 * stream to them).
 */
export function getAttachmentType(
  name: string,
  mediaType: string
): { kind: AttachmentKind; mediaType: string } | null {
  const dot = name.lastIndexOf('.')
  const extension = dot === -1 ? '' : name.slice(dot + 1).toLowerCase()
  const type = mediaType.split(';')[0].trim().toLowerCase()

  if (IMAGE_TYPES[extension]) {
    return { kind: 'image', mediaType: IMAGE_TYPES[extension] }
  }
  if (DOCUMENT_TYPES[extension]) {
    return { kind: 'document', mediaType: DOCUMENT_TYPES[extension] }
  }
  if (TEXT_EXTENSIONS.has(extension)) {
    return { kind: 'text', mediaType: 'text/plain' }
  }
  const imageType = Object.values(IMAGE_TYPES).find((value) => value === type)
  if (imageType) {
    return { kind: 'image', mediaType: imageType }
  }
  if (type.startsWith('text/') || TEXT_MEDIA_TYPES.has(type)) {
    return { kind: 'text', mediaType: 'text/plain' }
  }
  return null
}

function getSizeLimit(kind: AttachmentKind): number {
  if (kind === 'image') return ATTACHMENT_LIMITS.imageBytes
  if (kind === 'document') return ATTACHMENT_LIMITS.documentBytes
  return ATTACHMENT_LIMITS.textBytes
}

/**
 * Splits the files a user adds into those that fit the message's limits and
 * the reasons the others do not.
 */
export function vetAttachmentFiles(
  files: File[],
  current: Pick<FileUIPart, 'filename' | 'mediaType'>[]
): { accepted: File[]; rejected: AttachmentRejection[] } {
  const accepted: File[] = []
  const rejected: AttachmentRejection[] = []
  let count = current.length
  let documents = current.filter(
    (file) =>
      getAttachmentType(file.filename ?? '', file.mediaType)?.kind ===
      'document'
  ).length

  for (const file of files) {
    const type = getAttachmentType(file.name, file.type)
    if (!type) {
      rejected.push({ name: file.name, reason: 'type', limit: 0 })
      continue
    }
    const sizeLimit = getSizeLimit(type.kind)
    if (file.size > sizeLimit) {
      rejected.push({ name: file.name, reason: 'size', limit: sizeLimit })
      continue
    }
    if (count >= ATTACHMENT_LIMITS.files) {
      rejected.push({
        name: file.name,
        reason: 'count',
        limit: ATTACHMENT_LIMITS.files,
      })
      break
    }
    if (type.kind === 'document' && documents >= ATTACHMENT_LIMITS.documents) {
      rejected.push({
        name: file.name,
        reason: 'documents',
        limit: ATTACHMENT_LIMITS.documents,
      })
      continue
    }
    count++
    if (type.kind === 'document') documents++
    accepted.push(file)
  }

  return { accepted, rejected }
}

function withMediaType(dataUrl: string, mediaType: string): string {
  const [, encoded] = dataUrl.split(';base64,')
  return encoded === undefined ? dataUrl : `data:${mediaType};base64,${encoded}`
}

/**
 * The attachments of a submitted message, from the files PromptInput hands
 * over as data URLs.
 */
export function toMessageAttachments(
  files: FileUIPart[] = []
): MessageAttachment[] {
  const attachments: MessageAttachment[] = []
  files.forEach((file, index) => {
    const name = file.filename || `attachment-${index + 1}`
    const type = getAttachmentType(name, file.mediaType)
    if (!type) return
    attachments.push({
      id: nanoid(),
      kind: type.kind,
      name,
      mediaType: type.mediaType,
      url: withMediaType(file.url, type.mediaType),
    })
  })
  return attachments
}

export function decodeDataUrlText(dataUrl: string): string {
  const [, encoded = ''] = dataUrl.split(';base64,')
  const bytes = Uint8Array.from(atob(encoded), (char) => char.charCodeAt(0))
  return new TextDecoder().decode(bytes)
}

/**
 * The chat content of a message: its text alone, or its attachments followed
 * by its text. Text files are inlined under a line naming the file, images
 * are image_url parts and documents file parts.
 */
export function buildAttachmentContent(
  text: string,
  attachments: MessageAttachment[] = []
): string | ContentPart[] {
  const parts: ContentPart[] = []
  for (const attachment of attachments) {
    if (!attachment.url) continue
    if (attachment.kind === 'image') {
      parts.push({ type: 'image_url', image_url: { url: attachment.url } })
    } else if (attachment.kind === 'document') {
      parts.push({
        type: 'file',
        file: { filename: attachment.name, file_data: attachment.url },
      })
    } else {
      parts.push({
        type: 'text',
        text: `File: ${attachment.name}\n\n${decodeDataUrlText(attachment.url)}`,
      })
    }
  }

  if (parts.length === 0) return text
  if (text) parts.push({ type: 'text', text })
  return parts
}

/** What a saved conversation keeps of an attachment: no data. */
export function withoutAttachmentData<
  T extends { attachments?: MessageAttachment[] },
>(message: T): T {
  if (!message.attachments?.length) return message
  return {
    ...message,
    attachments: message.attachments.map(({ url: _url, ...rest }) => rest),
  }
}

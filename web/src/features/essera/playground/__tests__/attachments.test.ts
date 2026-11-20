import { describe, expect, it } from 'vitest'

import {
  ATTACHMENT_LIMITS,
  buildAttachmentContent,
  getAttachmentType,
  toMessageAttachments,
  vetAttachmentFiles,
  withoutAttachmentData,
  type MessageAttachment,
} from '../attachments'

function fileOfSize(name: string, type: string, size: number): File {
  const file = new File(['x'], name, { type })
  Object.defineProperty(file, 'size', { value: size })
  return file
}

function base64(text: string): string {
  return btoa(String.fromCharCode(...new TextEncoder().encode(text)))
}

describe('Attachment types', () => {
  it.each([
    ['photo.PNG', '', 'image', 'image/png'],
    ['pasted', 'image/webp', 'image', 'image/webp'],
    ['report.pdf', '', 'document', 'application/pdf'],
    [
      'notes.docx',
      'application/octet-stream',
      'document',
      'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    ],
    ['main.ts', 'video/mp2t', 'text', 'text/plain'],
    ['data.csv', 'text/csv', 'text', 'text/plain'],
    ['config', 'application/json', 'text', 'text/plain'],
  ])('%s (%s) is sent as %s %s', (name, type, kind, mediaType) => {
    expect(getAttachmentType(name, type)).toEqual({ kind, mediaType })
  })

  it.each([
    ['photo.bmp', 'image/bmp'],
    ['movie.mp4', 'video/mp4'],
    ['archive.zip', 'application/zip'],
  ])('%s is not supported', (name, type) => {
    expect(getAttachmentType(name, type)).toBeNull()
  })
})

describe('Vetting the files a user adds', () => {
  it('accepts an image of exactly 3.75 MB and refuses a larger one', () => {
    const { accepted, rejected } = vetAttachmentFiles(
      [
        fileOfSize('a.png', 'image/png', ATTACHMENT_LIMITS.imageBytes),
        fileOfSize('b.png', 'image/png', ATTACHMENT_LIMITS.imageBytes + 1),
      ],
      []
    )

    expect(accepted.map((file) => file.name)).toEqual(['a.png'])
    expect(rejected).toEqual([
      { name: 'b.png', reason: 'size', limit: ATTACHMENT_LIMITS.imageBytes },
    ])
  })

  it('refuses a document over 4.5 MB and an unsupported type', () => {
    const { accepted, rejected } = vetAttachmentFiles(
      [
        fileOfSize('big.pdf', 'application/pdf', 5 * 1024 * 1024),
        fileOfSize('a.zip', 'application/zip', 10),
      ],
      []
    )

    expect(accepted).toEqual([])
    expect(rejected.map((rejection) => rejection.reason)).toEqual([
      'size',
      'type',
    ])
  })

  it('counts the documents already attached against the five allowed', () => {
    const current = Array.from({ length: 4 }, (_, index) => ({
      filename: `${index}.pdf`,
      mediaType: 'application/pdf',
    }))

    const { accepted, rejected } = vetAttachmentFiles(
      [
        fileOfSize('5.pdf', 'application/pdf', 10),
        fileOfSize('6.pdf', 'application/pdf', 10),
        fileOfSize('a.png', 'image/png', 10),
      ],
      current
    )

    expect(accepted.map((file) => file.name)).toEqual(['5.pdf', 'a.png'])
    expect(rejected).toEqual([
      {
        name: '6.pdf',
        reason: 'documents',
        limit: ATTACHMENT_LIMITS.documents,
      },
    ])
  })

  it('stops at ten files with one rejection', () => {
    const files = Array.from({ length: 12 }, (_, index) =>
      fileOfSize(`${index}.png`, 'image/png', 10)
    )

    const { accepted, rejected } = vetAttachmentFiles(files, [])

    expect(accepted).toHaveLength(ATTACHMENT_LIMITS.files)
    expect(rejected).toEqual([
      { name: '10.png', reason: 'count', limit: ATTACHMENT_LIMITS.files },
    ])
  })
})

describe('Message content with attachments', () => {
  it('stays plain text without attachments', () => {
    expect(buildAttachmentContent('Hello', [])).toBe('Hello')
  })

  it('sends images, documents and inlined text files before the prompt', () => {
    const [image, document, text] = toMessageAttachments([
      {
        type: 'file',
        mediaType: 'image/png',
        filename: 'cat.png',
        url: 'data:image/png;base64,iVBO',
      },
      {
        type: 'file',
        mediaType: '',
        filename: 'Q3.pdf',
        url: 'data:application/octet-stream;base64,JVBERi0=',
      },
      {
        type: 'file',
        mediaType: 'text/markdown',
        filename: 'notes.md',
        url: `data:text/markdown;base64,${base64('# Café')}`,
      },
    ])

    expect(buildAttachmentContent('Compare', [image, document, text])).toEqual([
      { type: 'image_url', image_url: { url: 'data:image/png;base64,iVBO' } },
      {
        type: 'file',
        file: {
          filename: 'Q3.pdf',
          file_data: 'data:application/pdf;base64,JVBERi0=',
        },
      },
      { type: 'text', text: 'File: notes.md\n\n# Café' },
      { type: 'text', text: 'Compare' },
    ])
  })

  it('leaves out attachments a saved conversation no longer holds', () => {
    const saved: MessageAttachment = {
      id: '1',
      kind: 'image',
      name: 'cat.png',
      mediaType: 'image/png',
    }

    expect(buildAttachmentContent('And now?', [saved])).toBe('And now?')
  })

  it('saves the attachment name without its data', () => {
    const message = {
      key: 'm',
      attachments: [
        {
          id: '1',
          kind: 'image' as const,
          name: 'cat.png',
          mediaType: 'image/png',
          url: 'data:image/png;base64,iVBO',
        },
      ],
    }

    expect(withoutAttachmentData(message).attachments).toEqual([
      { id: '1', kind: 'image', name: 'cat.png', mediaType: 'image/png' },
    ])
    expect(message.attachments[0].url).toBe('data:image/png;base64,iVBO')
  })
})

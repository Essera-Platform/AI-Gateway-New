import { describe, expect, test } from 'vitest'

import { DEFAULT_CONFIG, DEFAULT_PARAMETER_ENABLED } from '../../../constants'
import type { Message } from '../../../types'
import {
  appendUserMessagePair,
  createRegeneratedMessages,
} from '../../message/conversation-message-utils'
import { completeAssistantMessage } from '../../message/message-streaming-utils'
import {
  updateAssistantMessageWithError,
  updateLastAssistantMessage,
} from '../../message/message-update-utils'
import { updateCurrentVersionContent } from '../../message/message-utils'
import { buildChatCompletionPayload } from '../payload-builder'

const bedrockError = 'AWS Bedrock: Bedrock is unable to process your request.'

function answer(messages: Message[], content: string): Message[] {
  return updateLastAssistantMessage(messages, (message) =>
    completeAssistantMessage(updateCurrentVersionContent(message, content))
  )
}

function fail(messages: Message[]): Message[] {
  return updateAssistantMessageWithError(messages, bedrockError, '503')
}

function payloadMessages(messages: Message[]) {
  return buildChatCompletionPayload(
    messages,
    DEFAULT_CONFIG,
    DEFAULT_PARAMETER_ENABLED
  ).messages
}

describe('buildChatCompletionPayload after a failed request', () => {
  test('a new prompt after an error sends neither the error nor the failed prompt', () => {
    const failed = fail(appendUserMessagePair([], 'Tell me a story'))
    const next = appendUserMessagePair(failed, 'hi')

    const sent = payloadMessages(next)

    expect(sent).toEqual([{ role: 'user', content: 'hi' }])
    expect(JSON.stringify(sent)).not.toContain(bedrockError)
  })

  test('answered turns before the error stay in the history', () => {
    const answered = answer(appendUserMessagePair([], 'Hello'), 'Hi there')
    const failed = fail(appendUserMessagePair(answered, 'Tell me a story'))
    const next = appendUserMessagePair(failed, 'hi')

    expect(payloadMessages(next)).toEqual([
      { role: 'user', content: 'Hello' },
      { role: 'assistant', content: 'Hi there' },
      { role: 'user', content: 'hi' },
    ])
  })

  test('regenerating the failed reply sends its prompt again', () => {
    const failed = fail(appendUserMessagePair([], 'Tell me a story'))
    const regenerated = createRegeneratedMessages(failed, failed[1].key)

    expect(regenerated).not.toBeNull()
    expect(payloadMessages(regenerated ?? [])).toEqual([
      { role: 'user', content: 'Tell me a story' },
    ])
  })
})

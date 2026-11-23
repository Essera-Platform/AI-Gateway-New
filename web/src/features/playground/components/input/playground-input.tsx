/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
// Modified by Essera, 2026-11: passes the selected model's parameter support to the parameter panel.
// Modified by Essera, 2026-11: files can be attached to a message.
// Modified by Essera, 2026-11: the design's composer card and its keyboard hint.
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  PromptInput,
  PromptInputFooter,
  PromptInputTextarea,
  type PromptInputMessage,
} from '@/components/ai-elements/prompt-input'
import { PendingAttachments } from '@/features/essera/playground/attachment-chips'
import {
  toMessageAttachments,
  type MessageAttachment,
} from '@/features/essera/playground/attachments'
import { useAttachmentFilter } from '@/features/essera/playground/use-attachment-filter'

import { getSubmittableInputText } from '../../lib'
import type { ModelParameterSupport } from '../../lib/parameters/model-parameters'
import type {
  ModelOption,
  GroupOption,
  ParameterEnabled,
  PlaygroundConfig,
} from '../../types'
import { PlaygroundInputControls } from './playground-input-controls'
import { PlaygroundInputTools } from './playground-input-tools'

interface PlaygroundInputProps {
  config: PlaygroundConfig
  onSubmit: (text: string, attachments: MessageAttachment[]) => void
  onStop?: () => void
  disabled?: boolean
  isGenerating?: boolean
  models: ModelOption[]
  modelValue: string
  onModelChange: (value: string) => void
  isModelLoading?: boolean
  groups: GroupOption[]
  groupValue: string
  onGroupChange: (value: string) => void
  hasMessages?: boolean
  onConfigChange: <K extends keyof PlaygroundConfig>(
    key: K,
    value: PlaygroundConfig[K]
  ) => void
  onClearMessages?: () => void
  onParameterEnabledChange: (
    key: keyof ParameterEnabled,
    value: boolean
  ) => void
  parameterEnabled: ParameterEnabled
  parameterSupport?: ModelParameterSupport | null
}

export function PlaygroundInput({
  config,
  onSubmit,
  onStop,
  disabled,
  isGenerating,
  models,
  modelValue,
  onModelChange,
  isModelLoading = false,
  groups,
  groupValue,
  onGroupChange,
  hasMessages = false,
  onConfigChange,
  onClearMessages,
  onParameterEnabledChange,
  parameterEnabled,
  parameterSupport,
}: PlaygroundInputProps) {
  const { t } = useTranslation()
  const [text, setText] = useState('')
  const filterAttachments = useAttachmentFilter()

  const handleSubmit = (message: PromptInputMessage) => {
    const submittableText = getSubmittableInputText(message, disabled)

    // PromptInput keeps the attachments when onSubmit throws.
    if (!submittableText) throw new Error('Nothing to submit')
    onSubmit(submittableText, toMessageAttachments(message.files))
    setText('')
  }

  return (
    <div className='grid shrink-0 gap-2 px-1'>
      <PromptInput
        className='relative'
        filterFiles={filterAttachments}
        groupClassName='bg-card dark:bg-card has-disabled:bg-card dark:has-disabled:bg-card has-disabled:opacity-100 border-border shadow-[0_12px_30px_-10px_rgba(15,23,42,0.10)] dark:shadow-none rounded-xl overflow-hidden transition-all duration-200 focus-within:border-primary/60 focus-within:ring-3 focus-within:ring-primary/15'
        onSubmit={handleSubmit}
      >
        <PendingAttachments />
        <PromptInputTextarea
          autoComplete='off'
          autoCorrect='off'
          autoCapitalize='off'
          spellCheck={false}
          className='placeholder:text-muted-foreground/80 min-h-20 px-5 pt-4 pb-3 text-[15px] leading-7 md:min-h-24'
          disabled={disabled}
          onChange={(event) => setText(event.target.value)}
          placeholder={t('Ask anything')}
          value={text}
        />

        <PromptInputFooter className='border-border/70 border-t px-3 py-2.5'>
          <PlaygroundInputControls
            disabled={disabled}
            groups={groups}
            groupValue={groupValue}
            isGenerating={isGenerating}
            isModelLoading={isModelLoading}
            models={models}
            modelValue={modelValue}
            onGroupChange={onGroupChange}
            onModelChange={onModelChange}
            onStop={onStop}
            text={text}
            tools={
              <PlaygroundInputTools
                config={config}
                disabled={disabled}
                hasMessages={hasMessages}
                onConfigChange={onConfigChange}
                onClearMessages={onClearMessages}
                onParameterEnabledChange={onParameterEnabledChange}
                parameterEnabled={parameterEnabled}
                parameterSupport={parameterSupport}
              />
            }
          />
        </PromptInputFooter>
      </PromptInput>
      <p className='text-muted-foreground hidden justify-end font-mono text-[11px] md:flex'>
        {t('Enter to send · Shift + Enter for a new line')}
      </p>
    </div>
  )
}

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
// Modified by Essera, 2026-11: the design's page header and the chat framed in a card.
// Modified by Essera, 2026-11: the parameters offered and sent follow the selected model.
import { useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { useEsseraManaged } from '@/features/essera/use-essera-managed'
import { usePricingData } from '@/features/pricing/hooks/use-pricing-data'

import { PlaygroundChat } from './components/chat/playground-chat'
import { PlaygroundInput } from './components/input/playground-input'
import {
  useChatHandler,
  usePlaygroundConversation,
  usePlaygroundOptions,
  usePlaygroundState,
} from './hooks'
import {
  effectiveParameterEnabled,
  modelParameterSupport,
} from './lib/parameters/model-parameters'
import type { ParameterEnabled } from './types'

export function Playground() {
  const { t } = useTranslation()
  const {
    config,
    parameterEnabled,
    messages,
    isLoadingMessages,
    models,
    groups,
    updateMessages,
    setModels,
    setGroups,
    updateConfig,
    updateParameterEnabled,
    clearMessages,
  } = usePlaygroundState()

  const { managed } = useEsseraManaged()
  const { models: pricedModels } = usePricingData(managed)
  const parameterSupport = useMemo(
    () => modelParameterSupport(pricedModels, config.model, managed),
    [pricedModels, config.model, managed]
  )
  const sentParameters = useMemo(
    () => effectiveParameterEnabled(parameterEnabled, parameterSupport),
    [parameterEnabled, parameterSupport]
  )
  const handleParameterEnabledChange = useCallback(
    (key: keyof ParameterEnabled, value: boolean) => {
      updateParameterEnabled(key, value)
      if (!value || !parameterSupport?.samplingExclusive) return
      if (key === 'temperature') updateParameterEnabled('top_p', false)
      if (key === 'top_p') updateParameterEnabled('temperature', false)
    },
    [parameterSupport, updateParameterEnabled]
  )

  const { sendChat, stopGeneration, isGenerating } = useChatHandler({
    config,
    parameterEnabled: sentParameters,
    onMessageUpdate: updateMessages,
  })

  const {
    editingMessageKey,
    handleSendMessage,
    handleRegenerateMessage,
    handleEditMessage,
    handleEditOpenChange,
    applyEdit,
    handleDeleteMessage,
  } = usePlaygroundConversation({
    messages,
    updateMessages,
    sendChat,
  })

  const handleClearMessages = () => {
    handleEditOpenChange(false)
    clearMessages()
  }

  const { isLoadingModels } = usePlaygroundOptions({
    currentGroup: config.group,
    currentModel: config.model,
    setGroups,
    setModels,
    updateConfig,
  })

  return (
    <div className='flex size-full min-h-0 flex-col gap-4 px-3 pt-3 pb-3 sm:px-6 sm:pt-6 sm:pb-6 lg:px-8'>
      <div className='flex shrink-0 flex-col gap-1'>
        <h2 className='flex items-center gap-2.5 text-lg font-bold tracking-tight sm:text-2xl'>
          <span
            aria-hidden='true'
            className='bg-primary size-2 shrink-0 rounded-full shadow-[0_0_0_4px_color-mix(in_oklch,var(--primary)_18%,transparent)]'
          />
          {t('Model Playground')}
        </h2>
        <p className='text-muted-foreground text-[13px] sm:text-sm'>
          {t(
            'Send test requests to the models your account can call. They count toward your usage and limits.'
          )}
        </p>
      </div>
      <div className='bg-card shadow-card relative flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border'>
        {/* Full-width scroll container: scrolling works even over side whitespace */}
        <div className='flex min-h-0 flex-1 flex-col overflow-hidden'>
          <PlaygroundChat
            messages={messages}
            isLoadingMessages={isLoadingMessages}
            onRegenerateMessage={handleRegenerateMessage}
            onEditMessage={handleEditMessage}
            onDeleteMessage={handleDeleteMessage}
            onSelectPrompt={handleSendMessage}
            isGenerating={isGenerating}
            editingKey={editingMessageKey}
            onCancelEdit={handleEditOpenChange}
            onSaveEdit={(newContent) => applyEdit(newContent, false)}
            onSaveEditAndSubmit={(newContent) => applyEdit(newContent, true)}
          />
        </div>

        {/* Input area: center content and constrain to the same container width */}
        <div className='mx-auto w-full max-w-4xl px-3 pb-3 sm:px-5 sm:pb-4'>
          <PlaygroundInput
            config={config}
            disabled={isGenerating}
            groups={groups}
            groupValue={config.group}
            isGenerating={isGenerating}
            isModelLoading={isLoadingModels}
            modelValue={config.model}
            models={models}
            onGroupChange={(value) => updateConfig('group', value)}
            onConfigChange={updateConfig}
            onClearMessages={handleClearMessages}
            onModelChange={(value) => updateConfig('model', value)}
            onParameterEnabledChange={handleParameterEnabledChange}
            parameterSupport={parameterSupport}
            onStop={stopGeneration}
            onSubmit={handleSendMessage}
            parameterEnabled={sentParameters}
            hasMessages={messages.length > 0}
          />
        </div>
      </div>
    </div>
  )
}

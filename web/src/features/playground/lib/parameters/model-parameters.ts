// Essera, 2026-11: the parameters the Playground offers and sends follow the
// selected model's supported_parameters from /api/pricing (essera/modelparams).
import type { ParameterEnabled } from '../../types'
import {
  PLAYGROUND_PARAMETER_CONTROLS,
  type PlaygroundParameterKey,
} from './playground-parameters'

export type ModelParameterSupport = {
  supported: readonly string[]
  samplingExclusive: boolean
}

type PricedModel = {
  model_name: string
  supported_parameters?: string[]
  sampling_exclusive?: boolean
}

const UNKNOWN_MODEL_SUPPORT: ModelParameterSupport = {
  supported: ['max_tokens'],
  samplingExclusive: false,
}

/** Null keeps every control: a gateway that is not managed lists no parameters. */
export function modelParameterSupport(
  models: readonly PricedModel[],
  model: string,
  managed: boolean
): ModelParameterSupport | null {
  if (!managed) return null
  const priced = models.find((item) => item.model_name === model)
  if (!priced?.supported_parameters) return UNKNOWN_MODEL_SUPPORT
  return {
    supported: priced.supported_parameters,
    samplingExclusive: priced.sampling_exclusive === true,
  }
}

export function isParameterSupported(
  key: PlaygroundParameterKey,
  support: ModelParameterSupport | null
): boolean {
  const hasControl = PLAYGROUND_PARAMETER_CONTROLS.some(
    (control) => control.key === key
  )
  return hasControl && (support === null || support.supported.includes(key))
}

export function supportedParameterControls(
  support: ModelParameterSupport | null
) {
  return PLAYGROUND_PARAMETER_CONTROLS.filter(
    (control) => support === null || support.supported.includes(control.key)
  )
}

/** What is sent: a parameter the model does not accept is never enabled. */
export function effectiveParameterEnabled(
  enabled: ParameterEnabled,
  support: ModelParameterSupport | null
): ParameterEnabled {
  const effective = { ...enabled }
  for (const key of Object.keys(effective) as PlaygroundParameterKey[]) {
    effective[key] = effective[key] && isParameterSupported(key, support)
  }
  if (support?.samplingExclusive && effective.temperature) {
    effective.top_p = false
  }
  return effective
}

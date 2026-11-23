type PricedModelAvailability = {
  model_name: string
  temporarily_unavailable?: boolean
}

/** The Playground's model options, each marked one with the note to show beside it. */
export function withAvailabilityNotes<T extends { value: string }>(
  options: T[],
  priced: readonly PricedModelAvailability[],
  note: string
): (T & { note?: string })[] {
  const unavailable = new Set(
    priced
      .filter((model) => model.temporarily_unavailable)
      .map((model) => model.model_name)
  )
  if (unavailable.size === 0) return options
  return options.map((option) =>
    unavailable.has(option.value) ? { ...option, note } : option
  )
}

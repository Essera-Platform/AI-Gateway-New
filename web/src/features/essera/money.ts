/** Micro amounts, such as one request's cost, keep this many decimals, as the Console's ledger shows them. */
const MICRO_AMOUNT_DIGITS = 6

/**
 * The fraction digits to show an amount with: `digits`, or six for a non-zero amount below one
 * cent, which would otherwise round to zero.
 */
export function amountFractionDigits(amount: number, digits: number): number {
  const abs = Math.abs(amount)
  return abs > 0 && abs < 0.01 ? Math.max(digits, MICRO_AMOUNT_DIGITS) : digits
}

/** `value` with `digits` decimals, or more for a micro amount, without trailing zeros past `digits`. */
export function toAmountFixed(value: number, digits: number): string {
  const shown = amountFractionDigits(value, digits)
  const fixed = value.toFixed(shown)
  if (shown === digits) return fixed
  const [whole, fraction] = fixed.split('.')
  const kept = fraction.replace(/0+$/, '').padEnd(digits, '0')
  return kept ? `${whole}.${kept}` : whole
}

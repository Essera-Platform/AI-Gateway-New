/**
 * Splits a displayed figure into the figure and the unit after it: "100.00 USD" into "100.00"
 * and " USD", "99.9%" into "99.9" and "%". A magnitude letter (K, M, B, T) stays with the
 * figure; text without a digit is all figure.
 */
export function splitFigure(text: string): { figure: string; unit: string } {
  const match = /^(.*\d[KMBT]?)(\D*)$/.exec(text)
  if (!match) return { figure: text, unit: '' }
  return { figure: match[1], unit: match[2] }
}

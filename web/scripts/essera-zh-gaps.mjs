// Essera, 2026-11: lists the interface strings that the Simplified Chinese locale lacks.
// Run from web/: `node scripts/essera-zh-gaps.mjs`. Exits 1 when there is a gap.
import fs from 'node:fs'
import path from 'node:path'

const SRC = path.resolve('src')
const LOCALES = path.join(SRC, 'i18n/locales')

// Strings that read the same in Chinese: product and protocol names, model IDs, license names.
const SAME_IN_CHINESE = new Set([
  'AI Gateway',
  'API',
  'Essera',
  'Frontend design and development by New API contributors.',
  'OpenAI · Anthropic',
])

function readLocale(name) {
  const data = JSON.parse(fs.readFileSync(path.join(LOCALES, name), 'utf8'))
  const stray = Object.keys(data).filter((key) => key !== 'translation')
  return { strings: data.translation ?? {}, stray }
}

function sourceFiles(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      return entry.name === '__tests__' ? [] : sourceFiles(full)
    }
    const isSource = /\.(ts|tsx)$/.test(entry.name)
    return isSource && !/\.test\.tsx?$/.test(entry.name) ? [full] : []
  })
}

// t('…') calls, and the `labelKey: '…'` style constants that are passed to t() later.
const LITERAL_KEY =
  /\b(?:t\(|(?:label|title|description|message|placeholder|tooltip)Key:)\s*(['"`])((?:\\.|(?!\1)[^\\])*?)\1/gs

function keysUsedInSource() {
  const used = new Map()
  for (const file of sourceFiles(SRC)) {
    const text = fs.readFileSync(file, 'utf8')
    for (const match of text.matchAll(LITERAL_KEY)) {
      const [, quote, raw] = match
      if (quote === '`' && raw.includes('${')) continue
      const key = raw.replaceAll(/\\(['"`\\])/g, '$1')
      if (!used.has(key)) used.set(key, path.relative(SRC, file))
    }
  }
  return used
}

const en = readLocale('en.json')
const zh = readLocale('zh.json')
const used = keysUsedInSource()

const gaps = new Map()
for (const key of en.stray) gaps.set(key, 'en.json, outside "translation"')
for (const key of Object.keys(en.strings)) {
  if (!(key in zh.strings)) gaps.set(key, 'en.json')
}
for (const [key, file] of used) {
  if (!(key in zh.strings)) gaps.set(key, file)
}
for (const [key, value] of Object.entries(zh.strings)) {
  if (value !== key || SAME_IN_CHINESE.has(key)) continue
  const englishValue = en.strings[key]
  if (englishValue !== undefined && englishValue !== key) continue
  if (key.split(' ').length < 4 || /^https?:|^"|[{[]"/.test(key)) continue
  gaps.set(key, 'zh.json, still English')
}

for (const [key, where] of gaps) console.log(`${where}\t${key}`)
console.log(`\n${gaps.size} strings missing or untranslated in zh.json`)
process.exitCode = gaps.size > 0 ? 1 : 0

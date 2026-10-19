// Mirrors apps/api/internal/platform/textnorm.Clean exactly: a title's
// suppression key is built from the cleaned text, and a key that differs by one
// zero-width character suppresses nothing.
const STRIP_RUNES = new Set([
  0x200b, 0x200c, 0x200d, 0x200e, 0x200f, 0x202a, 0x202b, 0x202c, 0x202d,
  0x202e, 0x2060, 0x2066, 0x2067, 0x2068, 0x2069, 0xfeff
])

const TRIM_CHARS = new Set([' ', '\t', '\n', '\r', '　'])

export const cleanText = (value: string) => {
  let stripped = ''
  for (const char of value) {
    if (!STRIP_RUNES.has(char.codePointAt(0) ?? 0)) {
      stripped += char
    }
  }
  const chars = [...stripped]
  let start = 0
  let end = chars.length
  while (start < end && TRIM_CHARS.has(chars[start]!)) {
    start++
  }
  while (end > start && TRIM_CHARS.has(chars[end - 1]!)) {
    end--
  }
  return chars.slice(start, end).join('')
}

export const workTitleIdentity = (kind: number, lang: string, title: string) =>
  `title:${kind}:${lang}:${cleanText(title)}`

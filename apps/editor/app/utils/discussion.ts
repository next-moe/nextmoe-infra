const HTML_ESCAPES: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;'
}

export const escapeHtml = (text: string) =>
  text.replace(/[&<>"']/g, (char) => HTML_ESCAPES[char] ?? char)

const CODE_SPAN = /(`[^`\n]+`)/
const IS_CODE_SPAN = /^`[^`\n]+`$/
const URL_PATTERN = /(https?:\/\/[^\s<>"'`，。！？、；：（）【】「」]+)/
const URL_TRAILING = /[.,;:!?)\]]+$/

const LINK_CLASS = 'text-primary-600 break-all hover:underline'
const MENTION_CLASS = 'text-primary-600 font-medium'
const CODE_CLASS = 'bg-default-100 rounded px-1 py-0.5 font-mono text-[0.85em]'

const renderLink = (url: string) => {
  const trailing = url.match(URL_TRAILING)?.[0] ?? ''
  const href = trailing ? url.slice(0, -trailing.length) : url
  return `<a href="${escapeHtml(href)}" target="_blank" rel="noopener noreferrer nofollow ugc" class="${LINK_CLASS}">${escapeHtml(href)}</a>${escapeHtml(trailing)}`
}

const renderText = (text: string) =>
  escapeHtml(text)
    .replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>')
    .replace(
      /(^|[\s(（])@([^\s@<>&（）()，。！？、]{1,32})/gu,
      `$1<span class="${MENTION_CLASS}">@$2</span>`
    )

export const renderDiscussionBody = (raw: string) =>
  raw
    .split(CODE_SPAN)
    .map((part) =>
      IS_CODE_SPAN.test(part)
        ? `<code class="${CODE_CLASS}">${escapeHtml(part.slice(1, -1))}</code>`
        : part
            .split(URL_PATTERN)
            .map((piece) =>
              /^https?:\/\//.test(piece) ? renderLink(piece) : renderText(piece)
            )
            .join('')
    )
    .join('')
    .replace(/\n/g, '<br>')

export const discussionSnippet = (raw: string, max = 60) => {
  const flat = raw
    .replace(/\*\*|`/g, '')
    .replace(/\s+/g, ' ')
    .trim()
  return flat.length > max ? `${flat.slice(0, max)}…` : flat
}

export const mentionsInBody = (
  body: string,
  candidates: { uid: string; name: string }[]
) =>
  candidates
    .filter((candidate) => body.includes(`@${candidate.name}`))
    .map((candidate) => candidate.uid)

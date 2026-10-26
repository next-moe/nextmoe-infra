export type MemberLabeler = (fieldKey: string, member: string) => string

const sameKey: MemberLabeler = (_fieldKey, member) => member

const MEMBER_WORDS: Record<string, string> = { URL: '链接' }

type Rule = [
  RegExp,
  (match: RegExpMatchArray, member: (raw?: string) => string) => string
]

const RULES: Rule[] = [
  [
    /^(?:(\w+) )?must not be (?:empty|null)$/,
    (m, member) => `${member(m[1])}不能为空`
  ],
  [/^(\w+) is required$/, (m, member) => `${member(m[1])}不能为空`],
  [
    /^(?:(\w+) )?must be at most (\d+) characters$/,
    (m, member) => `${member(m[1])}不能超过 ${m[2]} 个字符`
  ],
  [
    /^(?:(\w+) )?must be between (-?\d+) and (-?\d+)$/,
    (m, member) => `${member(m[1])}需要在 ${m[2]} 到 ${m[3]} 之间`
  ],
  [
    /^"([^"]*)" is not an allowed (?:original )?language$/,
    (m) => `不支持语言「${m[1]}」`
  ],
  [/^lang must be one of /, () => '请选择列表中的语言'],
  [/^"([^"]*)" is not an allowed platform$/, (m) => `不支持平台「${m[1]}」`],
  [
    /^only an alias \(kind 1\) may have an empty lang$/,
    () => '只有别名可以不选语言，请为这个标题选择语言'
  ],
  [/^must contain at least one title$/, () => '至少需要填写一个标题'],
  [
    /^must contain at least one official \(kind 0\) title$/,
    () => '至少需要一个「正式名」类型的标题'
  ],
  [/^must contain at most (\d+) \w+$/, (m) => `最多只能填写 ${m[1]} 条`],
  [/^at most (\d+) parents$/, (m) => `最多只能有 ${m[1]} 个上级`],
  [/^duplicate |appears twice$/, () => '和其他条目重复了'],
  [
    /must be an http:\/\/ or https:\/\/ URL$/,
    () => '链接需要以 http:// 或 https:// 开头'
  ],
  [
    /^(\w+) must be a positive id/,
    (m, member) => `请选择有效的${member(m[1])}`
  ],
  [/ is locked$/, () => '这一项已锁定，不接受修改']
]

const TECHNICAL =
  /must be (?:a|an|one of) |must be \d|unknown (?:key|field)|is not a submittable field|is not an identity key|is not the canonical form/

const stripPrefix = (fieldKey: string, detail: string) =>
  detail
    .replace(/^editing: field "[^"]+": /, '')
    .replace(new RegExp(`^${fieldKey.replace(/\./g, '\\.')}: `), '')
    .trim()

export const explainFieldError = (
  fieldKey: string,
  detail: string,
  memberLabel: MemberLabeler = sameKey
) => {
  const reason = stripPrefix(fieldKey, detail)
  const element = reason.match(/^element (\d+): (.*)$/)
  const rest = element ? element[2]! : reason
  const where = element ? `第 ${Number(element[1]) + 1} 条` : ''
  const member = (raw?: string) =>
    !raw || raw === fieldKey.split('.').pop()
      ? ''
      : (MEMBER_WORDS[raw] ?? memberLabel(fieldKey, raw))
  for (const [pattern, render] of RULES) {
    const match = rest.match(pattern)
    if (match) {
      const text = render(match, member)
      return where ? `${where}：${text}` : text
    }
  }
  if (TECHNICAL.test(rest)) {
    return `提交的数据格式不正确，可能是编辑器的问题，请刷新页面后重试；如果仍然出现，请把这条提示反馈给我们（${reason}）`
  }
  return where ? `${where}：${rest}` : rest
}

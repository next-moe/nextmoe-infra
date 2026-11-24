import type {
  CanonSheetCandidate,
  CanonSheetQuestion,
  CanonSheetValue,
  CanonTodoAnswer
} from '#shared/types/canon'

export const NEW_ENTITY = 'new'

export const showValue = ({ value, is }: Pick<CanonSheetValue, 'value' | 'is'>) => {
  if (typeof value === 'string') {
    return value.startsWith('ref:') ? `→ ${value.slice(4)}` : value
  }
  const text = JSON.stringify(value)
  return is ? `${is}（${text}）` : text
}

export const candidateOrigin = (candidate: CanonSheetCandidate) =>
  candidate.token.startsWith('legacy:') ? '旧库' : 'canon'

export const answerOptions = (question: CanonSheetQuestion) => [
  ...question.candidates.map((c) => ({
    value: c.token,
    label: `${candidateOrigin(c)} #${c.id} ${c.label}`
  })),
  ...(question.cites ? [] : [{ value: NEW_ENTITY, label: '都不是，新建' }])
]

export const answerOf = (choice: string): CanonTodoAnswer => {
  if (choice === NEW_ENTITY) {
    return { new: true }
  }
  const [origin, id] = choice.split(':')
  return origin === 'legacy' ? { adopt: id } : { use: id }
}

export const answersFrom = (
  questions: CanonSheetQuestion[],
  chosen: Record<string, string>,
  omitted: string[]
) => {
  const out: Record<string, CanonTodoAnswer> = {}
  for (const q of questions) {
    const choice = chosen[q.ref]
    if (choice && !omitted.includes(q.ref)) {
      out[q.ref] = answerOf(choice)
    }
  }
  return out
}

export const unanswered = (
  questions: CanonSheetQuestion[],
  chosen: Record<string, string>,
  omitted: string[]
) => questions.filter((q) => !chosen[q.ref] && !omitted.includes(q.ref))

import type {
  CanonTodo,
  CanonSheetCandidate,
  CanonSheetQuestion,
  CanonSheetValue,
  CanonTodoAnswer
} from '#shared/types/canon'

export const NEW_ENTITY = 'new'

const firstText = (todo: CanonTodo, entity: string, property: string) => {
  const found = todo.suggestion.changeset?.entities.find((e) => e.entity === entity)
  const value = found?.properties[property]?.value
  return typeof value === 'string' && !value.startsWith('ref:') ? value : ''
}

/** What the row is about: a title, else a volume's ISBN, else the field. */
export const todoTitle = (todo: CanonTodo) => {
  const c = todo.suggestion.candidate
  if (c) {
    const value = typeof c.value === 'string' ? c.value : JSON.stringify(c.value)
    return `${c.target.entity} #${c.target.id} · ${c.property} = ${value}`
  }
  const isbn = firstText(todo, 'book_release', 'isbn13')
  const title = firstText(todo, 'work_title', 'text')
  return [title, isbn].filter(Boolean).join(' · ') || todo.subject_key
}

export const todoAsks = (todo: CanonTodo) =>
  todo.questions.map((q) => `「${q.name}」是哪一个`).join('；')

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

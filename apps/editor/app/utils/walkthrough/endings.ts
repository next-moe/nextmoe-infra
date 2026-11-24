import type { WtEnding, WtRoute } from '#shared/types/walkthrough'
import {
  WT_OUTCOME_LABEL,
  WT_ROUTE_LABEL,
  WT_ROUTES,
  WT_OUTCOMES
} from '~/constants/walkthrough'

type RawEnding = Partial<WtEnding> & { ending_type?: string }

const routeByCount = (characters: string[]): WtRoute =>
  characters.length === 0
    ? 'common'
    : characters.length === 1
      ? 'heroine'
      : 'harem'

const fromEndingType = (
  type: string | undefined,
  characters: string[]
): Pick<WtEnding, 'route_type' | 'outcome' | 'after' | 'other'> => {
  const none = { route_type: null, outcome: null, after: false, other: false }
  switch (type) {
    case 'heroine':
      return {
        ...none,
        route_type: characters.length > 1 ? 'harem' : 'heroine',
        outcome: 'happy'
      }
    case 'harem':
      return { ...none, route_type: 'harem', outcome: 'happy' }
    case 'true':
    case 'normal':
    case 'bad':
      return { ...none, route_type: routeByCount(characters), outcome: type }
    case 'after':
      return { ...none, route_type: routeByCount(characters), after: true }
    case 'other':
      return { ...none, other: true }
    default:
      return none
  }
}

export const endingClassified = (e: WtEnding) =>
  e.route_type !== null || e.outcome !== null || e.after || e.other

export const normalizeEnding = (raw: RawEnding): WtEnding => {
  const { ending_type: endingType, ...rest } = raw
  const characters = Array.isArray(rest.characters) ? rest.characters : []
  const routes = WT_ROUTES.map((r) => r.value as string)
  const outcomes = WT_OUTCOMES.map((o) => o.value as string)
  const ending: WtEnding = {
    key: rest.key ?? '',
    name: rest.name ?? '',
    name_zh: rest.name_zh ?? null,
    route_type: routes.includes(rest.route_type ?? '')
      ? rest.route_type!
      : null,
    outcome: outcomes.includes(rest.outcome ?? '') ? rest.outcome! : null,
    after: rest.after === true,
    other: rest.other === true,
    other_label: rest.other_label ?? null,
    characters
  }
  return endingClassified(ending)
    ? ending
    : { ...ending, ...fromEndingType(endingType, characters) }
}

export const endingLabel = (e: WtEnding) => {
  if (e.other) return e.other_label ? `其他 · ${e.other_label}` : '其他'
  const parts = [
    e.route_type && WT_ROUTE_LABEL[e.route_type],
    e.outcome && WT_OUTCOME_LABEL[e.outcome],
    e.after && '后日谈'
  ].filter(Boolean)
  return parts.length ? parts.join(' · ') : '未分类'
}

import type { EditRefFamily } from '#shared/types/edit'
import { vocabLabel } from '~/constants/vocabLabels'

export interface SuppressedKeyParts {
  label: string
  refs: { family: EditRefFamily; id: string }[]
}

const TITLE_KINDS = ['official', 'alias', 'abbreviation']

export const suppressedKeyParts = (key: string): SuppressedKeyParts => {
  const [kind, ...rest] = key.split(':')
  switch (kind) {
    case 'tag':
      return { label: '', refs: [{ family: 'tag', id: rest[0] ?? '' }] }
    case 'roster':
      return { label: '', refs: [{ family: 'character', id: rest[0] ?? '' }] }
    case 'credit': {
      const [role, creditName, character] = rest
      const refs: SuppressedKeyParts['refs'] = [
        { family: 'role', id: role ?? '' },
        { family: 'credit_name', id: creditName ?? '' }
      ]
      if (character && character !== '0') {
        refs.push({ family: 'character', id: character })
      }
      return { label: '', refs }
    }
    case 'trait':
      return { label: '', refs: [{ family: 'trait', id: rest[0] ?? '' }] }
    case 'engine':
      return { label: '', refs: [{ family: 'engine', id: rest[0] ?? '' }] }
    case 'label':
    case 'relation':
      return {
        label: '',
        refs: [
          {
            family: 'company',
            id: (kind === 'label' ? rest[0] : rest[1]) ?? ''
          }
        ]
      }
    case 'link':
    case 'series':
      return { label: `${rest[0] ?? ''}：${rest.slice(1).join(':')}`, refs: [] }
    case 'cover':
    case 'screenshot':
      return {
        label: `${kind === 'cover' ? '封面' : '截图'} ${(rest[0] ?? '').slice(0, 12)}…`,
        refs: []
      }
    case 'intro':
      return {
        label: `${vocabLabel('intro_lang', rest[1] ?? '')}简介（资料源 ${rest[0] ?? ''}）`,
        refs: []
      }
    case 'alias': {
      const [, lang, ...name] = rest
      return {
        label: `${name.join(':')}${lang ? `（${vocabLabel('olang', lang)}）` : ''}`,
        refs: []
      }
    }
    case 'title': {
      const [titleKind, lang, ...text] = rest
      const kindLabel = vocabLabel(
        'title_kind',
        TITLE_KINDS[Number(titleKind)] ?? ''
      )
      const langLabel = lang ? vocabLabel('olang', lang) : '无语言'
      return {
        label: `${text.join(':')}（${langLabel} · ${kindLabel}）`,
        refs: []
      }
    }
    default:
      return { label: key, refs: [] }
  }
}

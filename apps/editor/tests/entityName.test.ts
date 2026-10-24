import { describe, expect, it } from 'vitest'
import { alternateName, pickName } from '../app/utils/entityName'

const work = {
  display_name: '枯れない世界と終わる花',
  olang: 'ja',
  localized: {
    en: { value: 'Karenai Sekai to Owaru Hana', is_machine: false },
    ja: { value: '枯れない世界と終わる花', is_machine: false },
    'zh-Hans': { value: '永不枯萎的世界与终结之花', is_machine: false }
  }
}

describe('entity names by display language', () => {
  it('shows the Chinese name first by default and keeps the original as the alternate', () => {
    expect(pickName(work, 'zh')).toBe('永不枯萎的世界与终结之花')
    expect(alternateName(work, 'zh')).toBe('枯れない世界と終わる花')
  })

  it('shows the original name when it is already in the chosen language', () => {
    expect(pickName(work, 'ja')).toBe('枯れない世界と終わる花')
    expect(alternateName(work, 'ja')).toBe('')
  })

  it('picks the English name when English is chosen', () => {
    expect(pickName(work, 'en')).toBe('Karenai Sekai to Owaru Hana')
  })

  it('does not trust a ja entry on an entity whose own language is unknown', () => {
    const hit = {
      display_name: '国崎往人',
      localized: { ja: { value: 'Kunisaki Yukito' } }
    }
    expect(pickName(hit, 'ja')).toBe('国崎往人')
  })

  it('prefers a human name over a machine translation', () => {
    const company = {
      display_name: 'Key',
      lang: 'ja',
      localized: {
        'zh-Hans': { value: '钥匙', is_machine: true },
        'zh-Hant': { value: 'Key社', is_machine: false }
      }
    }
    expect(pickName(company, 'zh')).toBe('Key社')
  })

  it('falls back to name_zh for traits and to the display name when nothing matches', () => {
    expect(pickName({ display_name: 'Hair', name_zh: '毛发' }, 'zh')).toBe(
      '毛发'
    )
    expect(pickName({ display_name: 'Hair', name_zh: '毛发' }, 'en')).toBe(
      'Hair'
    )
    expect(pickName({ display_name: '', name_zh: '中文版' }, 'ja')).toBe(
      '中文版'
    )
  })
})

import { describe, expect, it } from 'vitest'
import {
  EDIT_CONFIGS,
  ENTITY_META,
  isEditFamily
} from '../app/constants/editEntities'
import { ENTITY_FAMILY } from '../app/constants/catalog'
import { EDITOR_NAV } from '../app/constants/navigation'
import { suppressedKeyParts } from '../app/utils/editSuppression'
import { userChipOf } from '../app/utils/people'
import { rowIdentity } from '../shared/utils/editModel'

// Every field key the catalog's editing registry served when this test was
// written (apps/api editspec, feat/editor-platform 96677029). A key added server-side still
// renders through the generic fallback; this list makes sure the ones that
// already exist got a label and a group instead.
const REGISTERED_KEYS = [
  'catalog.character.aliases',
  'catalog.character.birthday_day',
  'catalog.character.birthday_month',
  'catalog.character.blood_type',
  'catalog.character.bust_cm',
  'catalog.character.cup',
  'catalog.character.description',
  'catalog.character.display_name',
  'catalog.character.figure_hash',
  'catalog.character.gender',
  'catalog.character.height_cm',
  'catalog.character.hip_cm',
  'catalog.character.image_hash',
  'catalog.character.instance_of',
  'catalog.character.intros',
  'catalog.character.lang',
  'catalog.character.latin',
  'catalog.character.traits',
  'catalog.character.waist_cm',
  'catalog.character.weight_kg',
  'catalog.credit_name.aliases',
  'catalog.credit_name.kind',
  'catalog.credit_name.lang',
  'catalog.credit_name.latin',
  'catalog.credit_name.link_visibility',
  'catalog.credit_name.name',
  'catalog.credit_name.note',
  'catalog.credit_name.person_id',
  'catalog.engine.aliases',
  'catalog.engine.intro',
  'catalog.engine.name',
  'catalog.label.aliases',
  'catalog.label.intros',
  'catalog.label.kind',
  'catalog.label.lang',
  'catalog.label.latin',
  'catalog.label.links',
  'catalog.label.logo_hash',
  'catalog.label.name',
  'catalog.label.note',
  'catalog.label.relations',
  'catalog.person.birth_day',
  'catalog.person.birth_month',
  'catalog.person.birth_year',
  'catalog.person.gender',
  'catalog.person.intros',
  'catalog.person.links',
  'catalog.person.photo',
  'catalog.person.primary_credit_name_id',
  'catalog.release.engine_id',
  'catalog.release.hidden',
  'catalog.release.kind',
  'catalog.release.lang',
  'catalog.release.platform',
  'catalog.release.released',
  'catalog.release.title',
  'catalog.series.intros',
  'catalog.series.name',
  'catalog.tag.intros',
  'catalog.work.content_rating',
  'catalog.work.covers',
  'catalog.work.credits',
  'catalog.work.display_name',
  'catalog.work.display_nsfw',
  'catalog.work.engine_ids',
  'catalog.work.intros',
  'catalog.work.labels',
  'catalog.work.links',
  'catalog.work.olang',
  'catalog.work.roster',
  'catalog.work.screenshots',
  'catalog.work.series_ids',
  'catalog.work.tag_ids',
  'catalog.work.titles',
  'catalog.release.freeware',
  'catalog.release.labels',
  'catalog.release.languages',
  'catalog.release.minage',
  'catalog.release.official',
  'catalog.release.platforms',
  'catalog.tag.kind',
  'catalog.tag.name',
  'catalog.tag.note',
  'catalog.tag.parents',
  'catalog.tag.sexual',
  'catalog.tag.tier',
  'catalog.trait.aliases',
  'catalog.trait.applicable',
  'catalog.trait.default_spoiler',
  'catalog.trait.description',
  'catalog.trait.description_zh',
  'catalog.trait.group_id',
  'catalog.trait.group_order',
  'catalog.trait.name',
  'catalog.trait.name_zh',
  'catalog.trait.parents',
  'catalog.trait.searchable',
  'catalog.trait.sexual',
  'catalog.work.covers_upstream',
  'catalog.work.platforms',
  'catalog.work.relations',
  'catalog.work.screenshots_upstream',
  'catalog.work.series_members',
  'catalog.work.tag_spoilers'
]

const configFor = (key: string) =>
  Object.values(EDIT_CONFIGS).find((config) =>
    key.startsWith(`${config.entityType}.`)
  )

describe('entity edit configs', () => {
  it('give every registered field a label and an existing group', () => {
    for (const key of REGISTERED_KEYS) {
      const config = configFor(key)
      expect(config, key).toBeDefined()
      const field = config!.fields[key]
      expect(field?.label, key).toBeTruthy()
      expect(
        config!.groups.some((group) => group.value === field!.group),
        key
      ).toBe(true)
    }
  })

  it('keep their field keys inside their own entity type', () => {
    for (const config of Object.values(EDIT_CONFIGS)) {
      for (const key of Object.keys(config.fields)) {
        expect(key.startsWith(`${config.entityType}.`), key).toBe(true)
      }
    }
  })

  it('always offer an "other" group for fields the config does not know', () => {
    for (const config of Object.values(EDIT_CONFIGS)) {
      expect(
        config.groups.some((group) => group.value === 'other'),
        config.family
      ).toBe(true)
    }
  })

  it('name a ref family wherever a ref control needs one', () => {
    for (const config of Object.values(EDIT_CONFIGS)) {
      for (const [key, field] of Object.entries(config.fields)) {
        if (field.control === 'ref' || field.control === 'refList') {
          expect(field.ref, key).toBeTruthy()
        }
      }
    }
  })

  it('agree with the entity metadata, the history families and the sidebar', () => {
    const navTargets = new Set(
      EDITOR_NAV.flatMap((group) => group.items)
        .filter((item) => item.ready)
        .map((item) => item.to)
    )
    for (const [family, meta] of Object.entries(ENTITY_META)) {
      expect(isEditFamily(family)).toBe(true)
      expect(EDIT_CONFIGS[meta.family].family).toBe(family)
      expect(ENTITY_FAMILY[family]?.path, family).toBe(meta.path)
      expect(navTargets.has(meta.path), family).toBe(true)
    }
  })
})

describe('row identities of the new list fields', () => {
  it('match the server identity rules used for duplicates', () => {
    expect(
      rowIdentity('catalog.character.traits', { trait_id: 12, spoiler: 1 })
    ).toBe('12')
    expect(
      rowIdentity('catalog.label.relations', { label_id: 7, relation: 3 })
    ).toBe('3:7')
    expect(
      rowIdentity('catalog.credit_name.aliases', {
        name: 'X',
        lang: 'ja',
        kind: 1
      })
    ).toBe('1:ja:X')
  })
})

describe('suppressed key display for the new identities', () => {
  it('resolves refs or a readable label', () => {
    expect(suppressedKeyParts('trait:12').refs).toEqual([
      { family: 'trait', id: '12' }
    ])
    expect(suppressedKeyParts('relation:3:7').refs).toEqual([
      { family: 'company', id: '7' }
    ])
    expect(suppressedKeyParts('label:9:0').refs).toEqual([
      { family: 'company', id: '9' }
    ])
    expect(suppressedKeyParts('link:web:https://a.example/x:y').label).toBe(
      'web：https://a.example/x:y'
    )
    expect(suppressedKeyParts('alias:1:ja:名前:二').label).toContain('名前:二')
  })
})

describe('people on edit records', () => {
  it('turns a user object into a KunUserChip user and keeps the fallback for none', () => {
    expect(
      userChipOf({
        object: 'user',
        id: '42',
        name: '鲲',
        avatar: null
      } as never)
    ).toEqual({ id: 42, name: '鲲', avatar: '' })
    expect(
      userChipOf({
        object: 'user',
        id: '7',
        name: '已注销#7',
        avatar: 'https://a.example/a.webp'
      } as never)
    ).toMatchObject({ name: '已注销#7', avatar: 'https://a.example/a.webp' })
    expect(userChipOf(null)).toBeNull()
  })
})

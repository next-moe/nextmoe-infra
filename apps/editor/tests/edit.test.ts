import { describe, expect, it } from 'vitest'
import type { CatalogSchemaField } from '../shared/types/catalog'
import type { EditVocabulary } from '../shared/types/edit'
import { cleanText, workTitleIdentity } from '../shared/utils/textnorm'
import {
  buildPatch,
  changedFieldKeys,
  fieldVocabSlot,
  fingerprint,
  memberVocabSlot,
  normalizeFieldValue,
  reconcileTitleSuppression,
  rowIdentity,
  sameValue,
  vocabTokenOf,
  vocabWireValues,
  vocabularyNamesOf
} from '../shared/utils/editModel'
import { validateField } from '../shared/utils/editValidate'
import { diffFieldList, diffLineRows } from '../shared/utils/diff'
import {
  editProblemFieldErrors,
  editProblemMessage,
  fieldKeyOfPointer,
  parseEditProblem
} from '../shared/utils/editProblem'
import { resolveEditControl } from '../shared/utils/editControl'
import {
  contextImages,
  contextProvenance,
  contextRosterIdentity,
  latestWriter,
  upstreamRowsOf
} from '../app/utils/editContextRows'
import { setImageCdnBase } from '../app/utils/imageCdn'
import { suppressedKeyParts } from '../app/utils/editSuppression'
import { moveItem } from '../app/composables/useListReorder'

const field = (over: Partial<CatalogSchemaField>): CatalogSchemaField =>
  ({
    key: 'catalog.work.display_name',
    field_type: 'text',
    diff_hint: 'inline',
    vocabulary: '',
    base: 0,
    nullable: false,
    deprecated: false,
    max_elements: 0,
    max_suppressed: 0,
    ...over
  }) as CatalogSchemaField

const TITLES = field({
  key: 'catalog.work.titles',
  field_type: 'list',
  diff_hint: 'items',
  max_elements: 100,
  element: {
    type: 'object',
    members: [
      {
        key: 'lang',
        type: 'enum',
        vocabulary: 'olang',
        base: 0,
        nullable: true
      },
      { key: 'title', type: 'text', vocabulary: '', base: 0, nullable: false },
      {
        key: 'kind',
        type: 'int',
        vocabulary: 'title_kind',
        base: 0,
        nullable: false
      },
      { key: 'latin', type: 'text', vocabulary: '', base: 0, nullable: true }
    ]
  }
})

const TITLES_SUPPRESSED = field({
  key: 'catalog.work.titles.suppressed',
  field_type: 'list',
  diff_hint: 'items',
  max_elements: 200,
  max_suppressed: 200,
  element: { type: 'text', members: [] }
})

const TAGS = field({
  key: 'catalog.work.tag_ids',
  field_type: 'list',
  diff_hint: 'items',
  max_elements: 3,
  element: { type: 'ref', members: [] }
})

const CREDITS = field({
  key: 'catalog.work.credits',
  field_type: 'list',
  diff_hint: 'items',
  max_elements: 500,
  element: {
    type: 'object',
    members: [
      { key: 'role_id', type: 'ref', vocabulary: '', base: 0, nullable: false },
      {
        key: 'credit_name_id',
        type: 'ref',
        vocabulary: '',
        base: 0,
        nullable: false
      },
      {
        key: 'character_id',
        type: 'ref',
        vocabulary: '',
        base: 0,
        nullable: true
      },
      { key: 'note', type: 'text', vocabulary: '', base: 0, nullable: true }
    ]
  }
})

const TITLE_KIND: EditVocabulary = {
  name: 'title_kind',
  values: [
    { value: 'official', display_name: 'Official' },
    { value: 'alias', display_name: 'Alias' },
    { value: 'abbreviation', display_name: 'Abbreviation' }
  ]
}

describe('textnorm mirror', () => {
  it('strips zero-width and bidi characters and trims ASCII and ideographic space', () => {
    expect(cleanText('　 夏​日口袋‮ \t')).toBe('夏日口袋')
  })

  it('keeps the emoji variation selector and inner spaces', () => {
    expect(cleanText(' ⚠️ a  b ')).toBe('⚠️ a  b')
  })

  it('builds the title identity from the cleaned text', () => {
    expect(workTitleIdentity(1, 'ja', ' サマポケ‍ ')).toBe(
      'title:1:ja:サマポケ'
    )
    expect(workTitleIdentity(0, '', 'X')).toBe('title:0::X')
  })
})

describe('value model', () => {
  it('compares values regardless of key order and undefined members', () => {
    expect(
      sameValue({ a: 1, b: [1, { c: 2 }] }, { b: [1, { c: 2 }], a: 1 })
    ).toBe(true)
    expect(sameValue({ a: 1, b: undefined }, { a: 1 })).toBe(true)
    expect(sameValue([1, 2], [2, 1])).toBe(false)
  })

  it('drops blank optional members and UI-only keys but keeps required ones', () => {
    expect(
      normalizeFieldValue(TITLES, [
        { lang: '', title: 'A', kind: 1, latin: '', _key: 'x' },
        { lang: 'ja', title: '', kind: 0 }
      ])
    ).toEqual([
      { lang: '', title: 'A', kind: 1 },
      { lang: 'ja', title: '', kind: 0 }
    ])
  })

  it('always sends an optional token enum as a string, as the snapshot does', () => {
    expect(
      normalizeFieldValue(TITLES, [
        { title: 'A', kind: 1 },
        { lang: null, title: 'B', kind: 1 }
      ])
    ).toEqual([
      { lang: '', title: 'A', kind: 1 },
      { lang: '', title: 'B', kind: 1 }
    ])
  })

  it('drops an empty optional ref the way the credits snapshot omits it', () => {
    expect(
      normalizeFieldValue(CREDITS, [
        { role_id: 1, credit_name_id: 2, character_id: null, note: '' }
      ])
    ).toEqual([{ role_id: 1, credit_name_id: 2 }])
  })

  it('trims text list elements and drops empty ones', () => {
    const links = field({
      key: 'catalog.work.links',
      field_type: 'list',
      element: { type: 'text', members: [] }
    })
    expect(normalizeFieldValue(links, [' https://a ', '', '  '])).toEqual([
      'https://a'
    ])
  })

  it('builds a patch holding only changed fields, normalized', () => {
    const base = {
      'catalog.work.display_name': 'A',
      'catalog.work.titles': [{ lang: 'ja', title: 'A', kind: 0 }],
      'catalog.work.tag_ids': [1, 2]
    }
    const draft = {
      'catalog.work.display_name': 'A',
      'catalog.work.titles': [
        { lang: 'ja', title: 'A', kind: 0 },
        { lang: '', title: 'B', kind: 1, latin: '' }
      ],
      'catalog.work.tag_ids': [1, 2]
    }
    const fields = [field({}), TITLES, TAGS]
    expect(changedFieldKeys(fields, base, draft)).toEqual([
      'catalog.work.titles'
    ])
    expect(buildPatch(fields, base, draft)).toEqual({
      'catalog.work.titles': [
        { lang: 'ja', title: 'A', kind: 0 },
        { lang: '', title: 'B', kind: 1 }
      ]
    })
  })

  it('treats a snapshot blank member and an absent one as unchanged', () => {
    const base = { 'catalog.work.titles': [{ lang: '', title: 'A', kind: 0 }] }
    const draft = { 'catalog.work.titles': [{ title: 'A', kind: 0 }] }
    expect(changedFieldKeys([TITLES], base, draft)).toEqual([])
  })

  it('ignores deprecated fields', () => {
    const deprecated = field({ key: 'catalog.work.old', deprecated: true })
    expect(
      changedFieldKeys(
        [deprecated],
        { 'catalog.work.old': 1 },
        { 'catalog.work.old': 2 }
      )
    ).toEqual([])
  })

  it('derives row identities per field', () => {
    expect(
      rowIdentity('catalog.work.titles', { kind: 1, lang: 'ja', title: ' x ' })
    ).toBe('title:1:ja:x')
    expect(
      rowIdentity('catalog.work.credits', {
        role_id: 3,
        credit_name_id: 9,
        character_id: null
      })
    ).toBe('3:9:0')
    expect(
      rowIdentity('catalog.work.roster', { character_id: 7, kind: 1 })
    ).toBe('7')
    expect(
      rowIdentity('catalog.work.covers', { image_hash: 'abc', kind: 'main' })
    ).toBe('abc')
    expect(rowIdentity('catalog.work.tag_ids', 12)).toBe('12')
  })

  it('encodes integer vocabularies as base plus index and token ones as the token', () => {
    const kind = memberVocabSlot(TITLES.element!.members![2]!)!
    expect(vocabWireValues(kind, TITLE_KIND).map((v) => v.wire)).toEqual([
      0, 1, 2
    ])
    expect(vocabTokenOf(kind, TITLE_KIND, 2)).toBe('abbreviation')
    const based = { ...kind, base: 1 }
    expect(vocabWireValues(based, TITLE_KIND)[0]!.wire).toBe(1)
    expect(vocabTokenOf(based, TITLE_KIND, 1)).toBe('official')
    const olang = fieldVocabSlot(
      field({
        field_type: 'enum',
        vocabulary: 'olang',
        encoding: 'token'
      } as Partial<CatalogSchemaField>)
    )!
    expect(olang.encoding).toBe('token')
    expect(vocabTokenOf(olang, undefined, 'ja')).toBe('ja')
    expect(vocabTokenOf(olang, undefined, null)).toBeNull()
  })

  it('collects every vocabulary a schema references', () => {
    expect(vocabularyNamesOf([TITLES, TAGS])).toEqual(['olang', 'title_kind'])
  })

  it('fingerprints values deterministically', () => {
    expect(fingerprint({ a: 1, b: 2 })).toBe(fingerprint({ b: 2, a: 1 }))
    expect(fingerprint({ a: 1 })).not.toBe(fingerprint({ a: 2 }))
  })
})

describe('title suppression', () => {
  const key = 'catalog.work.titles'
  const base = [
    { lang: 'ja', title: 'A', kind: 0 },
    { lang: '', title: 'B', kind: 1 }
  ]

  it('suppresses a title removed from the base set', () => {
    expect(reconcileTitleSuppression(key, base, [base[0]], [])).toEqual([
      'title:1::B'
    ])
  })

  it('lifts the suppression when the same title comes back', () => {
    expect(reconcileTitleSuppression(key, base, base, ['title:1::B'])).toEqual(
      []
    )
  })

  it('keeps older suppressions that no current row matches', () => {
    expect(
      reconcileTitleSuppression(key, base, base, ['title:1:en:old'])
    ).toEqual(['title:1:en:old'])
  })

  it('treats an edited title as a removal of the old text', () => {
    const edited = [{ lang: 'ja', title: 'A2', kind: 0 }, base[1]]
    expect(reconcileTitleSuppression(key, base, edited, [])).toEqual([
      'title:0:ja:A'
    ])
  })
})

describe('validation', () => {
  const vocabs = { title_kind: TITLE_KIND }

  it('requires non-nullable text', () => {
    expect(validateField(field({}), '  ')).toHaveLength(1)
    expect(validateField(field({ nullable: true }), '')).toHaveLength(0)
  })

  it('flags missing required members and out-of-range vocab codes', () => {
    const issues = validateField(TITLES, [{ title: '', kind: 7 }], vocabs)
    expect(issues.map((i) => i.member)).toEqual(['title', 'kind'])
  })

  it('flags duplicates by row identity', () => {
    const issues = validateField(
      TITLES,
      [
        { lang: 'ja', title: 'A', kind: 0 },
        { lang: 'ja', title: ' A ', kind: 0 }
      ],
      vocabs
    )
    expect(issues).toHaveLength(1)
    expect(issues[0]!.index).toBe(1)
  })

  it('enforces the element cap and ref shape', () => {
    expect(
      validateField(TAGS, [1, 2, 3, 4]).some((i) => i.message.includes('最多'))
    ).toBe(true)
    expect(validateField(TAGS, [1, 'x']).some((i) => i.index === 1)).toBe(true)
  })

  it('accepts a complete credit row with an empty optional character', () => {
    expect(
      validateField(CREDITS, [
        { role_id: 1, credit_name_id: 5, character_id: null }
      ])
    ).toEqual([])
    expect(validateField(CREDITS, [{ role_id: 1 }])[0]!.member).toBe(
      'credit_name_id'
    )
  })

  it('caps a suppression list by max_suppressed', () => {
    const keys = Array.from({ length: 201 }, (_, i) => `title:0:ja:${i}`)
    expect(validateField(TITLES_SUPPRESSED, keys)).toHaveLength(1)
  })
})

describe('diffs', () => {
  it('splits list changes into added, removed and changed rows', () => {
    const diff = diffFieldList(
      'catalog.work.roster',
      [
        { character_id: 1, kind: 1, spoiler: 0 },
        { character_id: 2, kind: 2, spoiler: 0 }
      ],
      [
        { character_id: 2, kind: 1, spoiler: 0 },
        { character_id: 3, kind: 3, spoiler: 0 }
      ]
    )
    expect(diff.added).toEqual([{ character_id: 3, kind: 3, spoiler: 0 }])
    expect(diff.removed).toEqual([{ character_id: 1, kind: 1, spoiler: 0 }])
    expect(diff.changed).toHaveLength(1)
    expect(diff.reordered).toBe(false)
  })

  it('notices a pure reorder', () => {
    const diff = diffFieldList('catalog.work.tag_ids', [1, 2, 3], [3, 1, 2])
    expect(diff.added).toEqual([])
    expect(diff.removed).toEqual([])
    expect(diff.reordered).toBe(true)
  })

  it('diffs text by line', () => {
    expect(diffLineRows('a\nb\nc', 'a\nx\nc')).toEqual([
      { type: 'same', text: 'a' },
      { type: 'del', text: 'b' },
      { type: 'add', text: 'x' },
      { type: 'same', text: 'c' }
    ])
    expect(diffLineRows('', 'new')).toEqual([{ type: 'add', text: 'new' }])
  })
})

describe('problem details', () => {
  const error = {
    statusCode: 422,
    data: {
      status: 422,
      code: 'VALIDATION_FAILED',
      title: 'Validation failed',
      detail: 'patch is invalid',
      errors: [
        {
          pointer: '/patch/catalog.work.titles',
          reason: 'UNKNOWN_VALUE',
          detail: 'element 0: kind must be 0-2'
        },
        { pointer: '/note', reason: 'TOO_LONG', detail: '' }
      ]
    }
  }

  it('maps field pointers to field keys', () => {
    expect(fieldKeyOfPointer('/patch/catalog.work.titles')).toBe(
      'catalog.work.titles'
    )
    expect(fieldKeyOfPointer('/patch/a~1b/0')).toBe('a/b')
    expect(fieldKeyOfPointer('/note')).toBeNull()
  })

  it('groups field errors and keeps the server detail', () => {
    const problem = parseEditProblem(error)
    expect(editProblemFieldErrors(problem)).toEqual({
      'catalog.work.titles': ['取值无效：element 0: kind must be 0-2']
    })
  })

  it('prefers a translated message for known codes', () => {
    expect(
      editProblemMessage(
        parseEditProblem({ data: { status: 429, code: 'QUOTA_EXCEEDED' } })
      )
    ).toContain('额度')
    expect(editProblemMessage(parseEditProblem({}))).toContain('网络')
    expect(editProblemMessage(parseEditProblem({ statusCode: 503 }))).toContain(
      '暂时不可用'
    )
  })
})

describe('control resolution', () => {
  it('honours the configured control first', () => {
    expect(
      resolveEditControl(TITLES, { label: '', group: '', control: 'titles' })
    ).toBe('titles')
  })

  it('falls back on the schema shape', () => {
    expect(resolveEditControl(field({}))).toBe('text')
    expect(
      resolveEditControl(field({ field_type: 'enum', vocabulary: 'olang' }))
    ).toBe('vocab')
    expect(resolveEditControl(field({ field_type: 'enum' }))).toBe('readonly')
    expect(resolveEditControl(field({ field_type: 'ref' }))).toBe('ref')
    expect(resolveEditControl(CREDITS)).toBe('objectList')
    expect(resolveEditControl(TAGS)).toBe('refList')
    expect(resolveEditControl(TITLES_SUPPRESSED)).toBe('stringList')
    expect(resolveEditControl(field({ field_type: 'date' }))).toBe('date')
    expect(resolveEditControl(field({ field_type: 'imagehash' }))).toBe('image')
  })
})

describe('upstream rows from the edit-context face', () => {
  const COVERS = field({
    key: 'catalog.work.covers',
    field_type: 'list',
    diff_hint: 'image',
    max_elements: 20,
    element: {
      type: 'object',
      members: [
        {
          key: 'image_hash',
          type: 'imagehash',
          vocabulary: '',
          base: 0,
          nullable: false
        },
        { key: 'kind', type: 'text', vocabulary: '', base: 0, nullable: true }
      ]
    }
  })
  const INTROS = field({
    key: 'catalog.work.intros',
    field_type: 'list',
    diff_hint: 'lines',
    max_elements: 10,
    element: {
      type: 'object',
      members: [
        { key: 'lang', type: 'enum', vocabulary: '', base: 0, nullable: false },
        { key: 'intro', type: 'text', vocabulary: '', base: 0, nullable: false }
      ]
    }
  })
  const row = (over: Record<string, unknown>) =>
    ({
      field_provenance: {},
      human_lane: false,
      identity: null,
      machine_translated: false,
      present: true,
      source: { key: 'vndb', display: 'VNDB' },
      suppressed: false,
      value: null,
      ...over
    }) as never
  const ctx = (key: string, rows: unknown[]) =>
    ({ key, provenance: [], rows, rows_truncated: false, value: null }) as never
  const fmt = {
    memberText: (_: unknown, value: unknown) =>
      value === null || value === undefined ? '' : `<${value}>`
  }
  const identity = (key: string) => (r: unknown) => rowIdentity(key, r)

  it('lists only present upstream rows and keeps their identity', () => {
    const rows = upstreamRowsOf(
      TAGS.key,
      TAGS,
      ctx(TAGS.key, [
        row({ identity: 'tag:4', value: { tag_id: 4, name: '萌' } }),
        row({
          human_lane: true,
          source: { key: 'curated', display: '人工' },
          value: { tag_id: 5 }
        }),
        row({ identity: 'tag:6', present: false, suppressed: true }),
        row({ identity: null, value: { tag_id: null, name: '未映射' } })
      ]),
      { label: '标签', group: 'tags', control: 'refList', ref: 'tag' },
      identity(TAGS.key),
      fmt
    )
    expect(rows).toHaveLength(2)
    expect(rows[0]).toMatchObject({
      identity: 'tag:4',
      match: '4',
      label: '萌',
      source: 'VNDB',
      ref: { family: 'tag', id: '4' }
    })
    expect(rows[1]).toMatchObject({ identity: null, label: '未映射' })
  })

  it('maps object rows onto the human value identity and names the main ref', () => {
    const [credit] = upstreamRowsOf(
      CREDITS.key,
      CREDITS,
      ctx(CREDITS.key, [
        row({
          identity: 'credit:1:422:8086',
          value: {
            role_id: 1,
            credit_name_id: 422,
            character_id: 8086,
            note: null
          }
        })
      ]),
      {
        label: '制作人员',
        group: 'credits',
        members: {
          role_id: { ref: 'role' },
          credit_name_id: { ref: 'credit_name' },
          character_id: { ref: 'character' }
        }
      },
      identity(CREDITS.key),
      fmt
    )
    expect(credit).toMatchObject({
      identity: 'credit:1:422:8086',
      match: '1:422:8086',
      ref: { family: 'credit_name', id: '422' },
      characterId: '8086'
    })
    expect(credit!.detail).toBe('<1>')
  })

  it('builds image rows from the configured CDN and intro rows with full text', () => {
    setImageCdnBase('https://cdn.example/')
    const [cover] = upstreamRowsOf(
      COVERS.key,
      COVERS,
      ctx(COVERS.key, [
        row({
          identity: 'cover:abcd1234',
          value: { image_hash: 'abcd1234', kind: 'main' }
        })
      ]),
      { label: '封面', group: 'media' },
      identity(COVERS.key),
      fmt
    )
    expect(cover).toMatchObject({
      match: 'abcd1234',
      imageUrl: 'https://cdn.example/ab/cd/abcd1234.webp'
    })
    const [intro] = upstreamRowsOf(
      INTROS.key,
      INTROS,
      ctx(INTROS.key, [
        row({
          identity: 'intro:3:ja',
          machine_translated: true,
          value: { lang: 'ja', intro: 'あ'.repeat(100) }
        })
      ]),
      { label: '简介', group: 'intros' },
      identity(INTROS.key),
      fmt
    )
    expect(intro!.text).toHaveLength(100)
    expect(intro!.detail!.endsWith('…')).toBe(true)
    expect(intro!.machine).toBe(true)
    setImageCdnBase(undefined)
  })

  it('collects roster identities, image hashes and provenance', () => {
    expect(
      contextRosterIdentity(
        ctx('catalog.work.roster', [
          row({ identity: 'roster:7', value: { character_id: 7 } }),
          row({ identity: null, value: { character_id: 8 } })
        ])
      )
    ).toEqual({ '7': 'roster:7' })

    const context = {
      fields: [
        ctx(COVERS.key, [row({ value: { image_hash: 'ffff0000' } })]),
        {
          key: 'catalog.work.olang',
          provenance: [
            {
              column: 'olang',
              history: [
                { source: 'curated', human: true, at: '2026-10-02T00:00:00Z' },
                { source: 'vndb', human: false, at: '2026-01-01T00:00:00Z' }
              ]
            }
          ],
          rows: null,
          rows_truncated: false,
          value: 'ja'
        }
      ]
    } as never
    expect(
      Object.keys(
        contextImages(
          [
            COVERS,
            field({ key: 'catalog.person.photo', field_type: 'imagehash' })
          ],
          {
            [COVERS.key]: [{ image_hash: 'aaaa1111' }],
            'catalog.person.photo': 'bbbb2222'
          },
          context
        )
      ).sort()
    ).toEqual(['aaaa1111', 'bbbb2222', 'ffff0000'])
    const provenance = contextProvenance(context)
    expect(Object.keys(provenance)).toEqual(['catalog.work.olang'])
    expect(latestWriter(provenance['catalog.work.olang'])).toMatchObject({
      source: 'curated',
      human: true
    })
  })
})

describe('suppressed key display', () => {
  it('parses known key shapes for display only', () => {
    expect(suppressedKeyParts('tag:12').refs).toEqual([
      { family: 'tag', id: '12' }
    ])
    expect(suppressedKeyParts('credit:1:422:0').refs).toEqual([
      { family: 'role', id: '1' },
      { family: 'credit_name', id: '422' }
    ])
    expect(suppressedKeyParts('title:1:ja:a:b').label).toContain('a:b')
    expect(suppressedKeyParts('mystery').label).toBe('mystery')
  })
})

describe('list reorder', () => {
  it('moves an item and ignores out-of-range moves', () => {
    expect(moveItem([1, 2, 3], 0, 2)).toEqual([2, 3, 1])
    const same = [1, 2]
    expect(moveItem(same, 0, 5)).toBe(same)
  })
})

describe('suppressed companions', () => {
  const field = {
    key: 'catalog.work.tag_ids.suppressed',
    field_type: 'list',
    element: { type: 'text' }
  } as unknown as CatalogSchemaField

  it('submits identity keys sorted by code point and unique, as the backend requires', () => {
    expect(
      normalizeFieldValue(field, [
        'tag:9',
        'tag:10',
        'tag:9',
        'title:1:ja:𝒜',
        'title:1:ja:Z'
      ])
    ).toEqual(['tag:10', 'tag:9', 'title:1:ja:Z', 'title:1:ja:𝒜'])
  })

  it('does not report a change when only the order of hidden rows differs', () => {
    expect(
      changedFieldKeys(
        [field],
        { [field.key]: ['tag:1', 'tag:2'] },
        { [field.key]: ['tag:2', 'tag:1'] }
      )
    ).toEqual([])
  })
})

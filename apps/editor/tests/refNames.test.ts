import { describe, expect, it, vi } from 'vitest'
import {
  REF_NAMES_PER_REQUEST,
  createRefNameBatcher,
  preferredRefLabel,
  secondaryRefLabel,
  type RefNameItem
} from '../app/utils/refNameBatcher'

const item = (key: string, display: string, zh?: string): RefNameItem => {
  const [family = '', id = ''] = key.split(':')
  return { key, family, id, display_name: display, name_zh: zh }
}

const answering = () =>
  vi.fn(async (keys: string[]) =>
    keys.filter((key) => !key.endsWith(':0')).map((key) => item(key, key))
  )

describe('ref name batcher', () => {
  it('coalesces requests from several families made in one tick into one call', async () => {
    const fetchKeys = answering()
    const batcher = createRefNameBatcher(fetchKeys, 0)
    const [tags, people] = await Promise.all([
      batcher.request(['tag:1', 'tag:2']),
      batcher.request(['person:9', 'tag:1'])
    ])
    expect(fetchKeys).toHaveBeenCalledTimes(1)
    expect(fetchKeys.mock.calls[0]![0]).toEqual(['tag:1', 'tag:2', 'person:9'])
    expect(tags.get('tag:2')?.display_name).toBe('tag:2')
    expect(people.get('tag:1')?.display_name).toBe('tag:1')
  })

  it('reuses a key that is already in flight instead of asking again', async () => {
    let answer: (items: RefNameItem[]) => void = () => {}
    const fetchKeys = vi.fn(
      () =>
        new Promise<RefNameItem[]>((resolve) => {
          answer = resolve
        })
    )
    const batcher = createRefNameBatcher(fetchKeys, 0)
    const first = batcher.request(['tag:1'])
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(fetchKeys).toHaveBeenCalledTimes(1)
    const second = batcher.request(['tag:1'])
    answer([item('tag:1', '同居')])
    const [a, b] = await Promise.all([first, second])
    expect(fetchKeys).toHaveBeenCalledTimes(1)
    expect(a.get('tag:1')?.display_name).toBe('同居')
    expect(b.get('tag:1')?.display_name).toBe('同居')
  })

  it('splits large batches at the face limit and answers missing keys with null', async () => {
    const fetchKeys = answering()
    const batcher = createRefNameBatcher(fetchKeys, 0)
    const keys = Array.from(
      { length: REF_NAMES_PER_REQUEST + 3 },
      (_, i) => `tag:${i}`
    )
    const out = await batcher.request(keys)
    expect(fetchKeys).toHaveBeenCalledTimes(2)
    expect(out.get('tag:0')).toBeNull()
    expect(out.get('tag:5')?.key).toBe('tag:5')
  })

  it('answers null for every key when the request fails', async () => {
    const batcher = createRefNameBatcher(async () => {
      throw new Error('429')
    }, 0)
    const out = await batcher.request(['tag:1', 'work:2'])
    expect([...out.values()]).toEqual([null, null])
  })
})

describe('preferred ref labels', () => {
  it('prefers a Chinese name for vocabulary-like families and keeps the original as a hint', () => {
    const trait = item('trait:1', 'Hair', '毛发')
    expect(preferredRefLabel(trait)).toBe('毛发')
    expect(secondaryRefLabel(trait)).toBe('Hair')
  })

  it('keeps the display name of proper nouns even when a Chinese one exists', () => {
    const character = item('character:7', '鳴瀬しろは', '鸣濑白羽')
    expect(preferredRefLabel(character)).toBe('鳴瀬しろは')
    expect(secondaryRefLabel(character)).toBe('')
  })

  it('falls back to the Chinese name when the display name is empty', () => {
    expect(preferredRefLabel(item('release:3', '', '中文版'))).toBe('中文版')
  })
})

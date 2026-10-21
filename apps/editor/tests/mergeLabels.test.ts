import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { MERGE_MOVED_LABELS } from '../app/constants/merge'

const SPEC = fileURLToPath(
  new URL('../../../docs/catalog/v2-openapi.yaml', import.meta.url)
)

const enumOf = (schema: string, property: string) => {
  const text = readFileSync(SPEC, 'utf8')
  const block = text.slice(text.indexOf(`\n    ${schema}:\n`))
  const prop = block.slice(block.indexOf(`\n        ${property}:\n`))
  const list = prop.slice(prop.indexOf('enum:\n') + 'enum:\n'.length)
  const values: string[] = []
  for (const line of list.split('\n')) {
    const match = /^ {12}- (\S+)$/.exec(line)
    if (!match) {
      break
    }
    values.push(match[1]!)
  }
  return values
}

describe('merge movement labels', () => {
  it('labels every kind the API can report', () => {
    const keys = enumOf('MergeMovedCount', 'key')
    expect(keys.length).toBeGreaterThan(10)
    for (const key of keys) {
      expect(MERGE_MOVED_LABELS, key).toHaveProperty(key)
    }
  })
})

import { describe, expect, it } from 'vitest'
import {
  roleOptionsOf,
  rolePickerOptions,
  type RoleListItem
} from '../app/utils/roleOptions'

const role = (
  id: string,
  name: string,
  en: string,
  deprecated = false
): RoleListItem => ({
  id,
  key: en.toLowerCase(),
  display_name: name,
  deprecated,
  localized: { en: { value: en } }
})

const ROLES = [
  role('247', '脚本', 'Scenario'),
  role('251', '脚本', 'Script/Screenplay', true),
  role('133', '制作助理', 'Associate Producer'),
  role('235', '制作助理', 'Production Assistant'),
  role('308', '原画', '', true)
]

describe('role picker options', () => {
  it('hides retired roles, so a name the registry merged appears once', () => {
    const labels = rolePickerOptions(roleOptionsOf(ROLES)).map((o) => o.label)
    expect(labels.filter((label) => label.startsWith('脚本'))).toEqual(['脚本'])
    expect(labels).not.toContain('原画（已停用）')
  })

  it('keeps a retired role visible while it is the current value', () => {
    const options = rolePickerOptions(roleOptionsOf(ROLES), '308')
    expect(options.find((o) => o.value === '308')?.label).toBe('原画（已停用）')
  })

  it('tells two live roles with one Chinese name apart by their English name', () => {
    const labels = rolePickerOptions(roleOptionsOf(ROLES)).map((o) => o.label)
    expect(labels).toContain('制作助理（Associate Producer）')
    expect(labels).toContain('制作助理（Production Assistant）')
  })

  it('keeps the plain name for resolving a credit row', () => {
    expect(roleOptionsOf(ROLES).find((o) => o.value === '133')?.label).toBe(
      '制作助理'
    )
  })
})

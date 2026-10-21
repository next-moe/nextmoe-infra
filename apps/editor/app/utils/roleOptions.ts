export interface RoleListItem {
  id: string
  key: string
  display_name: string
  deprecated?: boolean
  localized?: Record<string, { value?: string }>
}

export interface RoleOption {
  value: string
  label: string
  hint?: string
  deprecated?: boolean
}

export const roleOptionsOf = (items: RoleListItem[]): RoleOption[] => {
  const activeNames = new Map<string, number>()
  for (const role of items) {
    if (!role.deprecated) {
      activeNames.set(
        role.display_name,
        (activeNames.get(role.display_name) ?? 0) + 1
      )
    }
  }
  return items.map((role) => ({
    value: role.id,
    label: role.display_name,
    deprecated: Boolean(role.deprecated),
    hint: role.deprecated
      ? '已停用'
      : (activeNames.get(role.display_name) ?? 0) > 1
        ? role.localized?.en?.value || role.key
        : undefined
  }))
}

export const rolePickerOptions = (
  options: RoleOption[],
  selected?: string | null
) =>
  options
    .filter((option) => !option.deprecated || option.value === selected)
    .map((option) => ({
      value: option.value,
      label: option.hint ? `${option.label}（${option.hint}）` : option.label
    }))

const canonicalize = (value: unknown): unknown => {
  if (Array.isArray(value)) {
    return value.map(canonicalize)
  }
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.keys(value as Record<string, unknown>)
        .sort()
        .map((key) => [
          key,
          canonicalize((value as Record<string, unknown>)[key])
        ])
    )
  }
  return value === undefined ? null : value
}

export const canonicalJson = (value: unknown) =>
  JSON.stringify(canonicalize(value))

export const valuesEqual = (a: unknown, b: unknown) =>
  canonicalJson(a) === canonicalJson(b)

export const cloneValue = <T>(value: T): T =>
  value === undefined ? value : (JSON.parse(JSON.stringify(value)) as T)

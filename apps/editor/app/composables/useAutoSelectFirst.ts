const WIDE_QUERY = '(min-width: 1024px)'

export const useAutoSelectFirst = <T>(
  selected: Ref<T | null>,
  first: () => T | undefined,
  select: (item: T) => void
) => {
  watch(
    [selected, first],
    ([current, next]) => {
      if (!current && next && window.matchMedia(WIDE_QUERY).matches) {
        select(next)
      }
    },
    { immediate: true }
  )
}

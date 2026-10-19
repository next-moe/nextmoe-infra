export const useUnsavedGuard = (
  isDirty: MaybeRefOrGetter<boolean>,
  message = '还有未提交的修改，确定要离开吗？草稿会保留在本机。'
) => {
  const bypass = ref(false)

  const onBeforeUnload = (event: BeforeUnloadEvent) => {
    if (!bypass.value && toValue(isDirty)) {
      event.preventDefault()
    }
  }

  onMounted(() => window.addEventListener('beforeunload', onBeforeUnload))
  onBeforeUnmount(() =>
    window.removeEventListener('beforeunload', onBeforeUnload)
  )

  onBeforeRouteLeave(async () => {
    if (bypass.value || !toValue(isDirty)) {
      return true
    }
    return useKunAlert({
      title: '离开编辑页',
      message,
      type: 'warning',
      confirmText: '离开',
      cancelText: '继续编辑'
    })
  })

  const allowLeave = () => {
    bypass.value = true
  }

  return { allowLeave }
}

import type { TelemetryApp } from '~~/shared/types/telemetry'

const persistAppQuery = (path: string) =>
  path === '/telemetry' || path === '/telemetry/issues'

export const useTelemetryApps = async () => {
  const route = useRoute()
  const router = useRouter()

  const { data, status, error, refresh } = await useApiFetch<TelemetryApp[]>(
    '/admin/telemetry/apps',
    { key: 'telemetry-apps' },
    'telemetry'
  )

  const apps = computed(() => data.value ?? [])

  const selectedId = computed(() => {
    const n = Number(route.query.app)
    if (apps.value.some((a) => a.id === n)) return n
    return apps.value[0]?.id ?? 0
  })

  const selected = computed(
    () => apps.value.find((a) => a.id === selectedId.value) ?? null
  )

  const setApp = (id: number) => {
    if (!id || String(route.query.app) === String(id)) return
    router.replace({ query: { ...route.query, app: String(id) } })
  }

  watch(
    [apps, () => route.query.app, () => route.path],
    () => {
      if (!persistAppQuery(route.path)) return
      const first = apps.value[0]
      if (!first) return
      const n = Number(route.query.app)
      if (apps.value.some((a) => a.id === n)) return
      setApp(first.id)
    },
    { immediate: true }
  )

  const options = computed(() =>
    apps.value.map((a) => ({
      value: String(a.id),
      label: `${a.display_name}（${a.service_name}）`
    }))
  )

  const appModel = computed({
    get: () => (selectedId.value ? String(selectedId.value) : ''),
    set: (v: string) => {
      const n = Number(v)
      if (n > 0) setApp(n)
    }
  })

  const isLoading = computed(() => status.value === 'pending')

  return {
    apps,
    selectedId,
    selected,
    setApp,
    appModel,
    options,
    refresh,
    error,
    isLoading
  }
}

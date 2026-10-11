<script setup lang="ts">
interface Target {
  kind: 'characters' | 'credit-names' | 'companies' | 'works'
  id: string
}
interface WorkRef {
  id?: string
  display_name?: string
  content_rating?: string
}
interface AttachRow {
  work?: WorkRef
  roster_role?: string
  spoiler?: string
  voices?: { id: string; display_name: string }[]
  roles?: { role_key: string; role_name: string; character_id?: string | null }[]
}

const props = defineProps<{ apiKey: string; nsfw: boolean }>()
const target = defineModel<Target | null>({ required: true })

const open = computed({
  get: () => target.value !== null,
  set: (v: boolean) => {
    if (!v) target.value = null
  }
})

const KIND_LABEL: Record<Target['kind'], string> = {
  characters: '角色',
  'credit-names': '名义',
  companies: '厂牌 / 社团',
  works: '作品'
}

const ROW_CAP = 15

const loading = ref(false)
const error = ref('')
const data = ref<Record<string, unknown> | null>(null)
const rows = ref<AttachRow[]>([])
const rowsMore = ref(false)
const showRaw = ref(false)

const { read, failureMessage } = useCatalogRead()
const apiGet = <T,>(path: string, query: Record<string, string>) =>
  read<T>(path, query, props.apiKey)

// v2 entity details carry the identity core only; everything about what an
// entity is attached to lives on its own collection. Companies have no
// sub-resource for this — their reverse lookup is the works filter.
const attachmentRequest = (
  t: Target
): { path: string; query: Record<string, string> } | null => {
  const limit = String(ROW_CAP)
  const nsfw: Record<string, string> = props.nsfw ? { nsfw: 'true' } : {}
  switch (t.kind) {
    case 'characters':
      return {
        path: `v2/catalog/characters/${t.id}/appearances`,
        query: { limit, ...nsfw }
      }
    case 'credit-names':
      return {
        path: `v2/catalog/credit-names/${t.id}/credits`,
        query: { limit, ...nsfw }
      }
    case 'companies':
      return {
        path: 'v2/catalog/works',
        query: { company_id: t.id, limit, ...nsfw }
      }
    default:
      return null
  }
}

const normalizeRows = (kind: Target['kind'], items: unknown[]): AttachRow[] =>
  kind === 'companies'
    ? items.map((w) => ({ work: w as WorkRef }))
    : (items as AttachRow[])

watch(target, async (t) => {
  // Clearing on close blanks the panel for the length of KunModal's leave
  // transition — the old entity has to stay on screen while it animates out.
  if (!t) return
  data.value = null
  rows.value = []
  rowsMore.value = false
  error.value = ''
  showRaw.value = false
  loading.value = true
  try {
    data.value = await apiGet<Record<string, unknown>>(
      `v2/catalog/${t.kind}/${t.id}`,
      {
        ...(t.kind === 'characters' && { view: 'full' }),
        ...(props.nsfw && { nsfw: 'true' })
      }
    )
    const attach = attachmentRequest(t)
    if (attach) {
      const list = await apiGet<{ items?: unknown[]; next_cursor?: string }>(
        attach.path,
        attach.query
      )
      rows.value = normalizeRows(t.kind, list.items ?? [])
      rowsMore.value = Boolean(list.next_cursor)
    }
  } catch (e) {
    error.value = failureMessage(e, props.apiKey)
  } finally {
    loading.value = false
  }
})

const title = computed(() =>
  data.value ? (entityName(data.value) ?? `#${target.value?.id}`) : ''
)
const isR18 = computed(
  () =>
    (data.value as { content_rating?: string } | null)?.content_rating === 'r18'
)

const str = (key: string): string | null => {
  const v = data.value?.[key]
  return typeof v === 'string' && v ? v : null
}
const num = (key: string): number | null => {
  const v = data.value?.[key]
  return typeof v === 'number' ? v : null
}

const GENDER_LABEL: Record<string, string> = {
  male: '男',
  female: '女',
  other: '其他'
}

const attributes = computed(() => {
  const out: { label: string; value: string }[] = []
  const kind = target.value?.kind
  if (kind === 'characters') {
    const gender = str('gender')
    if (gender) out.push({ label: '性别', value: GENDER_LABEL[gender] ?? gender })
    const birthday = str('birthday')
    if (birthday) out.push({ label: '生日', value: birthday })
    const height = num('height_cm')
    if (height) out.push({ label: '身高', value: `${height} cm` })
    const weight = num('weight_kg')
    if (weight) out.push({ label: '体重', value: `${weight} kg` })
    const blood = str('blood_type')
    if (blood) out.push({ label: '血型', value: blood.toUpperCase() })
    const m = data.value?.measurements as Record<string, unknown> | null
    if (m) {
      const three = [m.bust_cm, m.waist_cm, m.hip_cm]
      if (three.every((v) => typeof v === 'number'))
        out.push({ label: '三围', value: three.join(' / ') })
      if (typeof m.cup === 'string' && m.cup)
        out.push({ label: '罩杯', value: m.cup })
    }
  }
  if (kind === 'companies') {
    const companyKind = str('company_kind')
    if (companyKind) out.push({ label: '类别', value: companyKind })
    const count = num('work_count')
    if (count !== null) out.push({ label: '作品数', value: String(count) })
  }
  if (kind === 'credit-names') {
    const personId = str('person_id')
    if (personId) out.push({ label: '归属人物', value: `person #${personId}` })
  }
  const latin = str('latin')
  if (latin && latin !== title.value) out.push({ label: '罗马字', value: latin })
  const olang = str('olang')
  if (olang) out.push({ label: '原语言', value: olang })
  return out
})

const rowsTitle = computed(() =>
  target.value?.kind === 'companies' ? '名下作品' : '关联作品'
)

const goWork = (id?: string) => {
  if (!id) return
  target.value = null
  navigateTo(`/explore/work/${id}${props.nsfw ? '?nsfw=1' : ''}`)
}
</script>

<template>
  <KunModal v-model="open" size="lg" aria-label="实体档案">
    <div class="max-h-[70vh] space-y-4 overflow-y-auto pr-1">
      <p v-if="loading" class="py-8 text-center text-sm text-default-400">
        拉取实体档案…
      </p>
      <div
        v-else-if="error"
        class="rounded-lg bg-danger-50 p-3 text-sm text-danger"
      >
        {{ error }}
      </div>

      <template v-else-if="data">
        <div class="flex flex-wrap items-center gap-2">
          <h2 class="text-lg font-semibold text-foreground">{{ title }}</h2>
          <KunChip color="default" variant="flat" size="xs">
            {{ KIND_LABEL[target?.kind ?? 'works'] }} #{{ target?.id }}
          </KunChip>
          <KunChip v-if="isR18" color="danger" variant="flat" size="xs">
            R18
          </KunChip>
        </div>

        <div v-if="attributes.length" class="flex flex-wrap gap-1.5">
          <KunChip
            v-for="a in attributes"
            :key="a.label"
            color="default"
            variant="flat"
            size="xs"
          >
            {{ a.label }} · {{ a.value }}
          </KunChip>
        </div>

        <div v-if="rows.length" class="space-y-1.5">
          <h3 class="text-sm font-semibold text-foreground">
            {{ rowsTitle }}（{{ rows.length
            }}<template v-if="rowsMore">，API 可翻页取更多</template>）
          </h3>
          <button
            v-for="(r, i) in rows"
            :key="`row-${i}`"
            type="button"
            class="flex w-full flex-wrap items-center gap-2 rounded-lg border border-default-200 px-3 py-2 text-left transition-colors hover:border-primary"
            @click="goWork(r.work?.id)"
          >
            <span class="min-w-0 flex-1 truncate text-sm text-foreground">
              {{ r.work?.display_name ?? `#${r.work?.id}` }}
            </span>
            <KunChip
              v-if="r.work?.content_rating === 'r18'"
              color="danger"
              variant="flat"
              size="xs"
            >
              R18
            </KunChip>
            <KunChip
              v-if="r.roster_role"
              color="default"
              variant="flat"
              size="xs"
            >
              {{ r.roster_role }}
            </KunChip>
            <span v-if="r.voices?.length" class="text-xs text-default-400">
              CV {{ r.voices.map((v) => v.display_name).join(' / ') }}
            </span>
            <span v-if="r.roles?.length" class="text-xs text-default-400">
              {{ r.roles.map((ro) => ro.role_name).join(' · ') }}
            </span>
          </button>
        </div>

        <div>
          <KunButton
            variant="light"
            color="default"
            size="xs"
            @click="showRaw = !showRaw"
          >
            {{ showRaw ? '收起完整数据' : '完整数据' }}
          </KunButton>
          <div
            v-if="showRaw"
            class="mt-1 max-h-64 overflow-auto rounded bg-default-100 p-2"
          >
            <ExploreJsonTree :data="data" />
          </div>
        </div>
      </template>
    </div>
  </KunModal>
</template>

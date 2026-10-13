<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type {
  ExploreAttachRow,
  ExploreEntityKind,
  ExploreEntityTarget,
  ExploreImage,
  ExploreIntro,
  ExploreWorkBrief
} from '~~/shared/types/explore'

const props = defineProps<{ apiKey: string; nsfw: boolean }>()
const target = defineModel<ExploreEntityTarget | null>({ required: true })

const open = computed({
  get: () => target.value !== null,
  set: (v: boolean) => {
    if (!v) target.value = null
  }
})

const KIND_LABEL: Record<ExploreEntityKind, string> = {
  characters: '角色',
  'credit-names': '名义',
  companies: '厂牌 / 社团',
  works: '作品'
}

const DETAIL_INCLUDE: Partial<Record<ExploreEntityKind, string>> = {
  characters: 'image,intros',
  'credit-names': 'photo,intros',
  companies: 'logo,intros'
}

const INTRO_ORDER = ['zh-Hans', 'zh-Hant', 'ja', 'en']

const ROW_CAP = 15
const INTRO_CLAMP = 160

const loading = ref(false)
const error = ref('')
const data = ref<Record<string, unknown> | null>(null)
const rows = ref<ExploreAttachRow[]>([])
const rowsMore = ref(false)
const showRaw = ref(false)
const introOpen = ref(false)

const { read, failureMessage } = useCatalogRead()
const apiGet = <T,>(path: string, query: Record<string, string>) =>
  read<T>(path, query, props.apiKey)

// v2 entity details carry the identity core only; everything about what an
// entity is attached to lives on its own collection. Companies have no
// sub-resource for this — their reverse lookup is the works filter.
const attachmentRequest = (
  t: ExploreEntityTarget
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

const normalizeRows = (
  kind: ExploreEntityKind,
  items: unknown[]
): ExploreAttachRow[] =>
  kind === 'companies'
    ? items.map((w) => ({ work: w as ExploreWorkBrief }))
    : (items as ExploreAttachRow[])

watch(target, async (t) => {
  // Clearing on close blanks the panel for the length of KunModal's leave
  // transition — the old entity has to stay on screen while it animates out.
  if (!t) return
  data.value = null
  rows.value = []
  rowsMore.value = false
  error.value = ''
  showRaw.value = false
  introOpen.value = false
  loading.value = true
  try {
    const include = DETAIL_INCLUDE[t.kind]
    data.value = await apiGet<Record<string, unknown>>(
      `v2/catalog/${t.kind}/${t.id}`,
      {
        ...(t.kind === 'characters' && { view: 'full' }),
        ...(include && { include }),
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
const subtitle = computed(() => {
  const name = data.value?.display_name
  return typeof name === 'string' && name !== title.value ? name : null
})
const isR18 = computed(
  () =>
    (data.value as { content_rating?: string } | null)?.content_rating === 'r18'
)

const portrait = computed(() => {
  const img = (data.value?.image ?? data.value?.photo ?? data.value?.logo) as
    ExploreImage | null | undefined
  return img?.url && isImageShown(img, props.nsfw) ? img : null
})

const intro = computed(() => {
  const list = (data.value?.intros as ExploreIntro[] | undefined) ?? []
  const rank = (lang: string) => {
    const i = INTRO_ORDER.indexOf(lang)
    return i < 0 ? INTRO_ORDER.length : i
  }
  const best = [...list].sort((a, b) => rank(a.lang) - rank(b.lang))[0]
  return best
    ? { ...best, text: best.value.replace(/\\\n/g, '\n').trim() }
    : null
})

const str = (key: string): string | null => {
  const v = data.value?.[key]
  return typeof v === 'string' && v ? v : null
}
const num = (key: string): number | null => {
  const v = data.value?.[key]
  return typeof v === 'number' ? v : null
}

const attributes = computed(() => {
  const out: { label: string; value: string }[] = []
  const kind = target.value?.kind
  if (kind === 'characters') {
    const gender = str('gender')
    if (gender) out.push({ label: '性别', value: vocabLabel('gender', gender) })
    const birthday = str('birthday')
    if (birthday) out.push({ label: '生日', value: birthday })
    const height = num('height_cm')
    if (height) out.push({ label: '身高', value: `${height} cm` })
    const weight = num('weight_kg')
    if (weight) out.push({ label: '体重', value: `${weight} kg` })
    const blood = str('blood_type')
    if (blood)
      out.push({ label: '血型', value: vocabLabel('blood_type', blood) })
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
    if (companyKind)
      out.push({
        label: '类别',
        value: vocabLabel('company_kind', companyKind)
      })
    const count = num('work_count')
    if (count !== null) out.push({ label: '作品数', value: String(count) })
  }
  if (kind === 'credit-names') {
    const personId = str('person_id')
    if (personId) out.push({ label: '归属人物', value: `person #${personId}` })
  }
  const latin = str('latin')
  if (latin && latin !== title.value)
    out.push({ label: '罗马字', value: latin })
  return out
})

const rowsTitle = computed(() =>
  target.value?.kind === 'companies' ? '名下作品' : '参与作品'
)

const goWork = (work: ExploreWorkBrief) => {
  target.value = null
  navigateTo(exploreWorkPath(work.id, props.nsfw))
}
</script>

<template>
  <KunModal v-model="open" size="xl" aria-label="实体档案">
    <div class="max-h-[75vh] space-y-5 overflow-y-auto pr-1">
      <div v-if="loading" class="flex gap-4">
        <KunSkeleton variant="rect" width="7rem" height="9rem" rounded="lg" />
        <div class="flex-1 space-y-2">
          <KunSkeleton variant="text" width="50%" height="1.25rem" />
          <KunSkeleton variant="text" />
          <KunSkeleton variant="text" width="80%" />
        </div>
      </div>
      <KunInfo
        v-else-if="error"
        color="danger"
        variant="flat"
        icon="lucide:circle-alert"
        :description="error"
      />

      <template v-else-if="data">
        <div class="flex gap-4">
          <div
            v-if="portrait"
            :class="
              cn(
                'border-default-200 bg-default-100 w-28 shrink-0 overflow-hidden rounded-xl border',
                target?.kind === 'companies' && 'p-2'
              )
            "
          >
            <KunImageNative
              :src="portrait.url"
              :alt="title"
              loading="eager"
              :class-name="
                cn(
                  'w-full',
                  target?.kind === 'companies'
                    ? 'aspect-square object-contain'
                    : 'aspect-[3/4] object-cover object-top'
                )
              "
            />
          </div>
          <div class="min-w-0 flex-1 space-y-2">
            <div>
              <div class="flex flex-wrap items-center gap-2">
                <h2 class="text-foreground text-xl font-semibold">
                  {{ title }}
                </h2>
                <KunChip v-if="isR18" color="danger" variant="flat" size="xs">
                  R18
                </KunChip>
              </div>
              <p v-if="subtitle" class="text-default-500 text-sm">
                {{ subtitle }}
              </p>
              <p class="text-default-400 mt-0.5 text-xs">
                {{ KIND_LABEL[target?.kind ?? 'works'] }} #{{ target?.id }}
              </p>
            </div>
            <dl
              v-if="attributes.length"
              class="grid grid-cols-2 gap-x-4 gap-y-1 text-sm sm:grid-cols-3"
            >
              <div v-for="a in attributes" :key="a.label" class="flex gap-2">
                <dt class="text-default-400 shrink-0">{{ a.label }}</dt>
                <dd class="text-foreground min-w-0 truncate">{{ a.value }}</dd>
              </div>
            </dl>
          </div>
        </div>

        <div v-if="intro" class="space-y-1.5">
          <p
            :class="
              cn(
                'text-default-600 text-sm leading-6 whitespace-pre-line',
                !introOpen && 'line-clamp-5'
              )
            "
          >
            {{ intro.text }}
          </p>
          <div class="text-default-400 flex items-center gap-2 text-xs">
            <span v-if="intro.source">
              来源：{{ vocabLabel('sources', intro.source) }}
            </span>
            <span v-if="intro.is_machine" class="text-warning">机翻</span>
            <KunButton
              v-if="intro.text.length > INTRO_CLAMP"
              variant="light"
              color="primary"
              size="xs"
              @click="introOpen = !introOpen"
            >
              {{ introOpen ? '收起' : '展开全文' }}
            </KunButton>
          </div>
        </div>

        <ExploreEntityWorkRows
          v-if="rows.length"
          :rows="rows"
          :title="rowsTitle"
          :more="rowsMore"
          @go="goWork"
        />

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
            class="bg-default-100 mt-1 max-h-64 overflow-auto rounded p-2"
          >
            <ExploreJsonTree :data="data" />
          </div>
        </div>
      </template>
    </div>
  </KunModal>
</template>

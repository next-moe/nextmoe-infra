<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { MergedRow } from '~/utils/mergedRows'
import {
  LINK_KIND_FROM_WIRE,
  LINK_KIND_LABELS,
  LINK_SOURCE_LABELS,
  classifyWorkLink,
  contextLinkUrl,
  linkFingerprint,
  siteIconOf,
  siteOfUrl,
  type LinkKind
} from '~/utils/externalLinks'

const props = defineProps<{ fieldKey: string }>()

const list = useMergedRows(() => props.fieldKey)
const { context, config, field, disabled, rows, summary } = list
const family = computed(() => context.config.family)
const isWork = computed(() => family.value === 'work')

interface LinkView {
  row: MergedRow
  url: string | null
  text: string
  siteKey: string
  siteLabel: string
  kind: LinkKind
  dead: boolean
  fingerprint: string
}

const viewOf = (row: MergedRow): LinkView => {
  if (row.lane === 'ghost') {
    return {
      row,
      url: null,
      text: list.ghostLabel(row),
      siteKey: 'web',
      siteLabel: '—',
      kind: 'related',
      dead: false,
      fingerprint: row.key
    }
  }
  if (row.lane === 'human') {
    const url = String(row.value ?? '')
    const site = siteOfUrl(url)
    const classified = isWork.value ? classifyWorkLink(url) : null
    return {
      row,
      url: site ? url : null,
      text: url,
      siteKey: site?.key ?? 'web',
      siteLabel: site?.label ?? '无效网址',
      kind: classified?.kind ?? 'related',
      dead: false,
      fingerprint: classified
        ? linkFingerprint(classified.source, classified.externalId)
        : linkFingerprint('web', url)
    }
  }
  const raw = (row.value ?? {}) as Record<string, unknown>
  const source = row.upstream?.sourceKey ?? ''
  const externalId = String(raw.external_id ?? row.upstream?.label ?? '')
  const url = contextLinkUrl(family.value, source, externalId)
  const site = url ? siteOfUrl(url) : null
  const internal = !url && ['curated', 'galgame_wiki', 'user'].includes(source)
  const siteLabel = internal
    ? '站内锚点'
    : source === 'official_site'
      ? '官网'
      : source === 'web'
        ? (site?.label ?? '网页')
        : (LINK_SOURCE_LABELS[source] ?? site?.label ?? source)
  return {
    row,
    url,
    text: url ?? (internal ? `#${externalId}` : externalId),
    siteKey: internal
      ? 'internal'
      : source === 'official_site'
        ? 'official_site'
        : (site?.key ?? source),
    siteLabel,
    kind: LINK_KIND_FROM_WIRE[Number(raw.link_kind)] ?? 'related',
    dead: raw.dead === true,
    fingerprint: linkFingerprint(source, externalId)
  }
}

const showHidden = ref(false)
const views = computed(() =>
  rows.value
    .filter((row) => showHidden.value || row.status !== 'hidden')
    .map(viewOf)
    .filter((view) => view.siteKey !== 'internal')
)
const hiddenCount = computed(
  () => rows.value.filter((row) => row.status === 'hidden').length
)
const groups = computed(() =>
  [
    {
      key: 'identity',
      label: '身份链接',
      hint: '决定这条资料对应哪个外部条目',
      items: views.value.filter((view) => view.kind !== 'related')
    },
    {
      key: 'related',
      label: '相关链接',
      hint: '官网、商店、社交账号等',
      items: views.value.filter((view) => view.kind === 'related')
    }
  ].filter((group) => group.items.length)
)

const IDENTITY_LOCK =
  '身份链接决定这条资料对应哪个外部条目，不能隐藏。认错了条目请发起合并请求或联系审核者。'

const draft = ref('')
const draftSite = computed(() => siteOfUrl(draft.value))
const draftClass = computed(() =>
  isWork.value && draftSite.value ? classifyWorkLink(draft.value) : null
)
const allFingerprints = computed(
  () => new Set(rows.value.map(viewOf).map((view) => view.fingerprint))
)
const draftDuplicate = computed(() => {
  if (!draftSite.value) {
    return false
  }
  const fp = draftClass.value
    ? linkFingerprint(draftClass.value.source, draftClass.value.externalId)
    : linkFingerprint('web', draft.value.trim())
  return allFingerprints.value.has(fp)
})
const cap = computed(() => field.value?.max_elements ?? 0)
const canAdd = computed(
  () =>
    Boolean(draftSite.value) &&
    !draftDuplicate.value &&
    (!cap.value || list.draftList.value.length < cap.value)
)
const add = () => {
  if (!canAdd.value) {
    return
  }
  list.append(draft.value.trim())
  draft.value = ''
}

const copy = async (text: string) => {
  try {
    await navigator.clipboard.writeText(text)
    useKunMessage('已复制', 'success')
  } catch {
    useKunMessage('复制失败', 'error')
  }
}
</script>

<template>
  <div class="@container space-y-4">
    <section v-for="group in groups" :key="group.key" class="space-y-1">
      <header class="flex items-baseline gap-2 px-2">
        <h4 class="text-sm font-medium">{{ group.label }}</h4>
        <span class="text-default-400 text-xs tabular-nums">
          {{ group.items.length }}
        </span>
        <span class="text-default-400 hidden text-xs @lg:inline">
          {{ group.hint }}
        </span>
      </header>
      <ul class="space-y-0.5">
        <li v-for="view in group.items" :key="view.row.key">
          <EditListRow
            :row="view.row"
            :source="sourceLabelOf(view.row)"
            :can-hide="list.canHide(view.row)"
            :can-remove="!disabled"
            :hide-source="view.row.lane === 'upstream'"
            :lock-reason="
              view.kind !== 'related' ? IDENTITY_LOCK : '这条链接不能单独隐藏'
            "
            remove-label="删除这条链接"
            @remove="list.removeAt(view.row.index)"
            @hide="list.hide(view.row)"
            @restore="list.restore(view.row)"
            @undo="list.undo(view.row)"
          >
            <div class="flex min-w-0 items-center gap-3">
              <span
                class="bg-default-100 text-default-500 flex size-7 shrink-0 items-center justify-center rounded-md"
              >
                <KunIcon :name="siteIconOf(view.siteKey)" class="size-4" />
              </span>
              <div class="min-w-0 flex-1 @xl:flex @xl:items-center @xl:gap-3">
                <span
                  class="text-default-700 block truncate text-sm @xl:w-32 @xl:shrink-0"
                  :title="view.siteLabel"
                >
                  {{ view.siteLabel }}
                </span>
                <a
                  v-if="view.url"
                  :href="view.url"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="text-default-500 hover:text-primary-600 @xl:text-default-700 block min-w-0 truncate text-xs hover:underline @xl:flex-1 @xl:text-sm"
                  :title="view.url"
                >
                  {{ view.text.replace(/^https?:\/\//, '') }}
                </a>
                <span
                  v-else
                  class="text-default-500 block min-w-0 truncate font-mono text-xs @xl:flex-1"
                  :title="view.text"
                >
                  {{ view.text }}
                </span>
              </div>
              <span class="flex shrink-0 items-center gap-1">
                <KunChip
                  v-if="view.kind === 'probable'"
                  size="xs"
                  variant="flat"
                  color="warning"
                >
                  {{ LINK_KIND_LABELS.probable }}
                </KunChip>
                <KunChip
                  v-if="view.dead"
                  size="xs"
                  variant="flat"
                  color="danger"
                >
                  已失效
                </KunChip>
              </span>
            </div>
            <template #actions>
              <KunButton
                v-if="view.url"
                size="xs"
                variant="light"
                color="default"
                is-icon-only
                aria-label="复制链接"
                :class-name="
                  cn('opacity-0 group-hover:opacity-100 focus:opacity-100')
                "
                @click="copy(view.url)"
              >
                <KunIcon name="lucide:copy" class="size-3.5" />
              </KunButton>
            </template>
          </EditListRow>
        </li>
      </ul>
    </section>

    <WorkspaceEmpty
      v-if="!groups.length"
      icon="lucide:link"
      title="还没有外链"
      description="在下方粘贴网址即可添加，已知站点会自动识别。"
      class-name="py-6"
    />

    <EditListHiddenToggle v-model="showHidden" :count="hiddenCount" />

    <form
      v-if="!disabled"
      class="border-default-200 space-y-2 rounded-lg border border-dashed p-3"
      @submit.prevent="add"
    >
      <div class="flex items-center gap-2">
        <KunInput
          v-model="draft"
          placeholder="粘贴网址，例如 https://vndb.org/v1234"
          :aria-label="`添加${config?.label ?? '外链'}`"
          class="min-w-0 flex-1"
          size="sm"
        />
        <KunButton type="submit" variant="flat" size="sm" :disabled="!canAdd">
          <KunIcon name="lucide:plus" class="size-4" />
          添加
        </KunButton>
      </div>
      <p v-if="draft.trim()" class="flex flex-wrap items-center gap-2 text-xs">
        <template v-if="draftSite">
          <span class="text-default-600 inline-flex items-center gap-1">
            <KunIcon :name="siteIconOf(draftSite.key)" class="size-3.5" />
            {{ draftSite.label }}
          </span>
          <span v-if="draftClass?.kind === 'probable'" class="text-warning-700">
            会作为待确认的身份链接写入，由审核者确认
          </span>
          <span v-else class="text-default-500">相关链接</span>
          <span v-if="draftDuplicate" class="text-danger-600">
            这条链接已经存在
          </span>
        </template>
        <span v-else class="text-danger-600"
          >请填写以 http:// 或 https:// 开头的完整网址</span
        >
      </p>
      <p v-else class="text-default-400 text-xs">
        显示 {{ summary.visible }} 条
        <template v-if="summary.upstream">
          · {{ summary.upstream }} 条来自资料源</template
        >
      </p>
    </form>
  </div>
</template>

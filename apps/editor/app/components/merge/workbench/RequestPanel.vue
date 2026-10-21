<script setup lang="ts">
import { MERGE_FIELD_LABELS, MERGE_TABLE_LABELS } from '~/constants/merge'
import { mergeTimeline, noteEntries } from '~/utils/merge/timeline'

const workbench = useMergeWorkbench()
const { request, undoReport, dirty } = workbench

const now = ref(Date.now())
let ticker: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  ticker = setInterval(() => (now.value = Date.now()), 30_000)
})
onBeforeUnmount(() => clearInterval(ticker))

const steps = computed(() =>
  request.value ? mergeTimeline(request.value, now.value) : []
)
const entries = computed(() =>
  request.value ? noteEntries(request.value.note) : []
)
const reason = computed(
  () => entries.value.find((entry) => entry.kind === 'reason')?.text ?? ''
)
const decisions = computed(() =>
  entries.value.filter((entry) => entry.kind !== 'reason')
)

const DECISION_TEXT: Record<string, string> = {
  approved: '批准了合并',
  rejected: '驳回了合并',
  withdrawn: '撤回了请求',
  amended: '修改了合并选择',
  undone: '撤销了合并',
  system: ''
}

const tableLabel = (table: string) => MERGE_TABLE_LABELS[table] ?? table
const moved = computed(() =>
  Object.entries(request.value?.moved ?? {}).sort((a, b) => b[1] - a[1])
)
const chosen = computed(() =>
  Object.keys(request.value?.field_resolution ?? {}).map(
    (key) => MERGE_FIELD_LABELS[key] ?? key
  )
)
const live = computed(
  () => request.value?.state === 'open' || request.value?.state === 'approved'
)
</script>

<template>
  <div v-if="request" class="space-y-4">
    <div class="border-default-200 bg-content1 rounded-xl border p-4 md:p-5">
      <MergeWorkbenchTimeline :steps="steps" :now="now" />
    </div>

    <div class="grid gap-4 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
      <div
        class="border-default-200 bg-content1 space-y-3 rounded-xl border p-4"
      >
        <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
          <HistoryAuthor
            :user="request.proposed_by"
            :uid="request.proposed_by_uid"
            :origin="request.origin_site"
            size="sm"
          />
          <span class="text-default-400">
            提交于 {{ formatDateTime(request.proposed_at) }}
          </span>
        </div>
        <p
          v-if="reason"
          class="bg-default-50 rounded-lg p-3 text-sm leading-6 whitespace-pre-line"
        >
          {{ reason }}
        </p>
        <ul v-if="decisions.length" class="space-y-1.5 text-sm">
          <li
            v-for="(entry, index) in decisions"
            :key="index"
            class="text-default-600 flex items-start gap-2"
          >
            <KunIcon
              name="lucide:corner-down-right"
              class="text-default-400 mt-0.5 size-4 shrink-0"
            />
            <span>
              <template v-if="entry.uid">
                审核者 #{{ entry.uid }} {{ DECISION_TEXT[entry.kind] }}
              </template>
              <span v-if="entry.text" class="text-default-500">
                {{ entry.uid ? '：' : '' }}{{ entry.text }}
              </span>
            </span>
          </li>
        </ul>
      </div>

      <div
        class="border-default-200 bg-content1 space-y-2 rounded-xl border p-4 text-sm"
      >
        <p class="font-medium">请求里的选择</p>
        <p class="text-default-600">
          显示名等基本信息：{{
            chosen.length ? `采用被并入方的${chosen.join('、')}` : '保留方优先'
          }}
        </p>
        <p class="text-default-600">
          合并后隐藏的重复发行：{{ request.hide_release_ids.length }} 个
        </p>
        <p v-if="request.amended_by_uid" class="text-default-500 text-xs">
          审核者
          {{ request.amended_by?.name ?? `#${request.amended_by_uid}` }}
          于 {{ formatDateTime(request.amended_at) }} 修改过这些选择
        </p>
        <p
          v-if="dirty"
          class="text-primary-700 bg-primary-50 rounded-lg px-2 py-1.5 text-xs"
        >
          你在下方改动了选择，尚未保存。
        </p>
      </div>
    </div>

    <div
      v-if="!live && moved.length"
      class="border-default-200 bg-content1 space-y-3 rounded-xl border p-4"
    >
      <p class="text-sm font-medium">执行时移到保留方的数据</p>
      <div class="flex flex-wrap gap-2">
        <span
          v-for="[table, count] in moved"
          :key="table"
          class="bg-default-100 text-default-700 rounded-full px-3 py-1 text-sm"
        >
          {{ tableLabel(table) }} {{ count }}
        </span>
      </div>
    </div>

    <div
      v-if="undoReport"
      class="border-success-200 bg-success-50 space-y-2 rounded-xl border p-4 text-sm"
    >
      <p class="text-success-800 font-medium">撤销完成</p>
      <p class="text-success-800">
        移回被并入方：{{
          Object.entries(undoReport.restored)
            .map(([table, count]) => `${tableLabel(table)} ${count}`)
            .join('、') || '无'
        }}
      </p>
      <p
        v-if="Object.keys(undoReport.not_restored).length"
        class="text-warning-800"
      >
        未能移回（合并时与保留方合为一条，或之后已被删除）：{{
          Object.entries(undoReport.not_restored)
            .map(([table, count]) => `${tableLabel(table)} ${count}`)
            .join('、')
        }}
      </p>
      <p v-if="undoReport.kept_fields.length" class="text-warning-800">
        保留方的{{
          undoReport.kept_fields
            .map((key) => MERGE_FIELD_LABELS[key] ?? key)
            .join('、')
        }}在合并后被人改过，保持现状。
      </p>
    </div>

    <MergeWorkbenchDiscussion :request-id="request.id" />
  </div>
</template>

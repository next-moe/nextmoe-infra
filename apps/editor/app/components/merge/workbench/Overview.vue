<script setup lang="ts">
import { ENTITY_META } from '~/constants/editEntities'
import {
  countAfter,
  demotedAnchors,
  duplicateReleases,
  mergedTitles
} from '~/utils/merge/plan'

const emit = defineEmits<{
  'set-pair': [next: { source?: string | null; target?: string | null }]
}>()

const workbench = useMergeWorkbench()
const { preview, previewError, previewLoading, request, family } = workbench

const meta = computed(() => ENTITY_META[family.value])
const editable = computed(() => workbench.mode === 'compose')

const swap = () =>
  emit('set-pair', {
    source: workbench.targetId.value,
    target: workbench.sourceId.value
  })

const stats = computed(() => {
  const p = preview.value
  if (!p || p.blockers.length) {
    return []
  }
  const out: { icon: string; text: string }[] = []
  if (family.value === 'work') {
    const moving = p.releases.filter((r) => r.side === 'source').length
    const dups = duplicateReleases(p).length
    out.push({
      icon: 'lucide:package',
      text: `发行 +${moving}${dups ? `（${dups} 个重复）` : ''}`
    })
    const added =
      mergedTitles(p.titles).length -
      p.titles.filter((t) => t.side === 'target').length
    out.push({ icon: 'lucide:heading', text: `标题 +${added}` })
  }
  const demoted = demotedAnchors(p).length
  if (demoted) {
    out.push({ icon: 'lucide:link', text: `${demoted} 个资料源对应降级` })
  }
  const ratings = p.counts.find((c) => c.key === 'user_ratings')
  if (ratings?.on_source) {
    out.push({
      icon: 'lucide:star',
      text: `评分 ${ratings.on_target} → ${countAfter(ratings)}`
    })
  }
  return out
})
</script>

<template>
  <MergeWorkbenchSection section-key="overview">
    <MergeWorkbenchRequestPanel v-if="request" />

    <div class="grid items-stretch gap-3 md:grid-cols-[1fr_auto_1fr]">
      <MergeWorkbenchPairCard
        side="source"
        :entity-id="workbench.sourceId.value"
        :editable="editable"
        @pick="(id) => emit('set-pair', { source: id })"
      />
      <div class="flex items-center justify-center">
        <KunButton
          v-if="editable"
          variant="bordered"
          size="sm"
          is-icon-only
          aria-label="交换方向"
          :disabled="!workbench.sourceId.value && !workbench.targetId.value"
          @click="swap"
        >
          <KunIcon name="lucide:arrow-left-right" class="size-4" />
        </KunButton>
        <KunIcon
          v-else
          name="lucide:arrow-right"
          class="text-default-400 size-5 rotate-90 md:rotate-0"
        />
      </div>
      <MergeWorkbenchPairCard
        side="target"
        :entity-id="workbench.targetId.value"
        :editable="editable"
        @pick="(id) => emit('set-pair', { target: id })"
      />
    </div>

    <p
      v-if="editable"
      class="text-default-500 flex items-start gap-1.5 text-sm"
    >
      <KunIcon name="lucide:info" class="mt-0.5 size-4 shrink-0" />
      通常保留资料更完整、被引用更多的一方。被并入的{{
        meta.label
      }}编号以后会自动跳转到保留的{{ meta.label }}，下游站点也跟随跳转。
    </p>

    <KunInfo
      v-if="previewError"
      color="danger"
      variant="flat"
      icon="lucide:circle-x"
      title="无法计算合并预览"
      :description="previewError"
    />

    <template v-if="preview">
      <KunInfo
        v-if="preview.open_request_id && editable"
        color="primary"
        variant="flat"
        icon="lucide:git-pull-request"
        title="这两个条目已经有一条待审核的合并请求"
      >
        <NuxtLink
          :to="`/merge/${preview.open_request_id}`"
          class="text-primary-700 text-sm hover:underline"
        >
          查看合并请求 #{{ preview.open_request_id }}
        </NuxtLink>
      </KunInfo>

      <KunInfo
        v-for="blocker in preview.blockers"
        :key="blocker.code"
        color="danger"
        variant="flat"
        icon="lucide:octagon-x"
        title="不能这样合并"
        :description="blocker.message"
      />

      <div
        v-if="preview.warnings.length"
        class="border-warning-200 bg-warning-50 rounded-xl border p-4"
      >
        <p class="text-warning-800 flex items-center gap-2 text-sm font-medium">
          <KunIcon name="lucide:triangle-alert" class="size-4" />
          合并前请先看这些
        </p>
        <ul class="text-warning-800 mt-2 space-y-1.5 pl-6 text-sm">
          <li
            v-for="(warning, index) in preview.warnings"
            :key="index"
            class="list-disc"
          >
            {{ warning.message }}
          </li>
        </ul>
      </div>

      <div
        v-if="preview.facts.length"
        class="border-default-200 bg-content1 rounded-xl border p-4"
      >
        <p class="flex items-center gap-2 text-sm font-medium">
          <KunIcon name="lucide:search-check" class="text-success-600 size-4" />
          数据里找到的依据
        </p>
        <ul class="text-default-600 mt-2 space-y-1.5 pl-6 text-sm">
          <li v-for="fact in preview.facts" :key="fact" class="list-disc">
            {{ fact }}
          </li>
        </ul>
      </div>

      <div v-if="stats.length" class="flex flex-wrap gap-2">
        <span
          v-for="stat in stats"
          :key="stat.text"
          class="bg-default-100 text-default-700 inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-sm"
        >
          <KunIcon :name="stat.icon" class="text-default-500 size-4" />
          {{ stat.text }}
        </span>
      </div>
    </template>
    <div v-else-if="previewLoading" class="flex gap-2">
      <KunSkeleton
        v-for="n in 3"
        :key="n"
        variant="rect"
        width="8rem"
        height="1.75rem"
        class-name="rounded-full"
      />
    </div>
  </MergeWorkbenchSection>
</template>

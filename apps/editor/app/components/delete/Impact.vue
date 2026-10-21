<script setup lang="ts">
import type { CatalogDeletionPreview } from '#shared/types/catalog'
import { SITE_LABELS } from '~/constants/catalog'
import { WRITER_LABELS } from '~/constants/editEntities'

const props = defineProps<{
  preview: CatalogDeletionPreview | null
  loading?: boolean
}>()

const sourceLabel = (key: string) => WRITER_LABELS[key] ?? key

const blocking = computed(() =>
  (props.preview?.anchors ?? []).filter((a) => a.kind !== 'related')
)
const related = computed(() =>
  (props.preview?.anchors ?? []).filter((a) => a.kind === 'related')
)

const userData = computed(() => {
  const data = props.preview?.user_data
  if (!data) {
    return []
  }
  return [
    { label: '评分', count: data.ratings, icon: 'lucide:star' },
    { label: '收藏', count: data.folder_items, icon: 'lucide:folder-heart' },
    { label: '游玩时长', count: data.playtimes, icon: 'lucide:timer' },
    { label: '游玩状态', count: data.work_states, icon: 'lucide:gamepad-2' }
  ]
})
const userDataTotal = computed(() =>
  userData.value.reduce((sum, item) => sum + item.count, 0)
)
</script>

<template>
  <div class="grid gap-3 md:grid-cols-3">
    <section
      class="border-default-200 space-y-2 rounded-lg border p-3"
      aria-label="资料源对应"
    >
      <h3 class="flex items-center gap-1.5 text-sm font-medium">
        <KunIcon name="lucide:anchor" class="text-default-500 size-4" />
        资料源对应
      </h3>
      <template v-if="props.loading && !props.preview">
        <KunSkeleton variant="text" width="80%" />
        <KunSkeleton variant="text" width="60%" />
      </template>
      <template v-else-if="props.preview">
        <div v-if="blocking.length" class="flex flex-wrap gap-1.5">
          <KunChip
            v-for="anchor in blocking"
            :key="`${anchor.source}:${anchor.external_id}`"
            size="sm"
            variant="flat"
            :color="anchor.kind === 'identity' ? 'primary' : 'warning'"
          >
            {{ sourceLabel(anchor.source) }} {{ anchor.external_id }}
            <template v-if="anchor.kind === 'probable'">（待确认）</template>
          </KunChip>
        </div>
        <p class="text-default-500 text-xs leading-relaxed">
          <template v-if="blocking.length">
            这些对应会保留下来，删除后导入任务不会再建出这部作品。
          </template>
          <template v-else>
            没有资料源对应，删除后不会被任何导入重新建出来。
          </template>
          <template v-if="related.length">
            另有 {{ related.length }} 个相关链接随作品一起隐藏。
          </template>
        </p>
      </template>
    </section>

    <section
      class="border-default-200 space-y-2 rounded-lg border p-3"
      aria-label="用户数据"
    >
      <h3 class="flex items-center gap-1.5 text-sm font-medium">
        <KunIcon name="lucide:users" class="text-default-500 size-4" />
        用户数据
      </h3>
      <template v-if="props.loading && !props.preview">
        <KunSkeleton variant="text" width="70%" />
        <KunSkeleton variant="text" width="50%" />
      </template>
      <template v-else-if="props.preview">
        <ul class="grid grid-cols-2 gap-x-3 gap-y-1 text-sm">
          <li
            v-for="item in userData"
            :key="item.label"
            class="flex items-center justify-between gap-2"
          >
            <span class="text-default-500 inline-flex items-center gap-1">
              <KunIcon :name="item.icon" class="size-3.5" />
              {{ item.label }}
            </span>
            <span class="tabular-nums">{{ item.count }}</span>
          </li>
        </ul>
        <p class="text-default-500 text-xs leading-relaxed">
          {{
            userDataTotal
              ? '这些记录保留但不显示，作品恢复时一并回来。'
              : '还没有用户在这部作品上留下记录。'
          }}
        </p>
      </template>
    </section>

    <section
      class="border-default-200 space-y-2 rounded-lg border p-3"
      aria-label="编辑与站点"
    >
      <h3 class="flex items-center gap-1.5 text-sm font-medium">
        <KunIcon name="lucide:git-pull-request" class="text-default-500 size-4" />
        编辑与站点
      </h3>
      <template v-if="props.loading && !props.preview">
        <KunSkeleton variant="text" width="75%" />
        <KunSkeleton variant="text" width="55%" />
      </template>
      <ul v-else-if="props.preview" class="text-default-600 space-y-1 text-sm">
        <li class="flex items-center gap-1.5">
          <KunIcon name="lucide:package" class="text-default-400 size-3.5" />
          {{ props.preview.releases }} 个发行随作品一起隐藏
        </li>
        <li class="flex items-center gap-1.5">
          <KunIcon name="lucide:inbox" class="text-default-400 size-3.5" />
          <template v-if="props.preview.open_proposals">
            {{ props.preview.open_proposals }} 条待审提案会被驳回
          </template>
          <template v-else>没有待审的编辑提案</template>
        </li>
        <li class="flex items-center gap-1.5">
          <KunIcon name="lucide:globe" class="text-default-400 size-3.5" />
          <template v-if="props.preview.claim_site">
            由 {{ SITE_LABELS[props.preview.claim_site] ?? props.preview.claim_site }}
            认领，该站作品页会显示已删除
          </template>
          <template v-else>没有站点认领</template>
        </li>
      </ul>
    </section>
  </div>
</template>

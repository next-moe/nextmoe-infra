<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { SPLIT_COUNT_LABELS, SPLIT_SOURCE_LABELS } from '~/constants/split'
import { EDIT_LANGUAGE_LABELS } from '~/constants/vocabLabels'
import { releaseDateText } from '~/utils/merge/plan'
import { countOf, parseRefKey } from '~/utils/split/plan'
import { splitParticipants } from '~/utils/split/timeline'

const workbench = useSplitWorkbench()
const { preview, previewLoading, selection, draft, request } = workbench

const TABS = [
  { value: 'result', textValue: '结果', icon: 'lucide:git-fork' },
  { value: 'request', textValue: '请求', icon: 'lucide:file-text' },
  { value: 'discussion', textValue: '讨论', icon: 'lucide:messages-square' }
]
const tab = ref('result')

const releases = computed(() => preview.value?.releases ?? [])
const staying = computed(() =>
  releases.value.filter((r) => !selection.value.releases.includes(r.id))
)
const leaving = computed(() =>
  releases.value.filter((r) => selection.value.releases.includes(r.id))
)
const tombstones = computed(() => leaving.value.filter((r) => r.tombstone))
const promoted = computed(() => selection.value.promote.map(parseRefKey))
const PREVIEW_LIMIT = 6
const showAllStaying = ref(false)
const stayingShown = computed(() =>
  showAllStaying.value ? staying.value : staying.value.slice(0, PREVIEW_LIMIT)
)
</script>

<template>
  <section class="min-h-0 flex-col" aria-label="拆分结果">
    <header
      :class="
        cn(
          'border-default-200 shrink-0 border-b',
          request ? 'px-2 pt-1' : 'px-4 py-3'
        )
      "
    >
      <KunTab
        v-if="request"
        v-model="tab"
        :items="TABS"
        variant="underlined"
        name="split-result-tabs"
      />
      <h2 v-else class="flex items-center gap-2 text-sm font-semibold">
        <span class="bg-primary-500 size-2 rounded-full" />
        拆分后
      </h2>
    </header>

    <div class="min-h-0 flex-1 overflow-y-auto px-4 py-4">
      <SplitWorkbenchRequestPanel v-if="request && tab === 'request'" compact />
      <DiscussionThread
        v-else-if="request && tab === 'discussion'"
        anchor-kind="split_request"
        :anchor-id="request.id"
        :participants="splitParticipants(request)"
      />
      <div v-else class="space-y-4">
        <SplitWorkbenchFacts />
        <CommonSkeletonRows v-if="previewLoading && !preview" :rows="6" />

        <template v-else-if="preview">
          <div
            class="border-default-200 bg-content1 space-y-3 rounded-xl border p-4"
          >
            <p class="flex items-center gap-2 text-xs">
              <span class="bg-success-500 size-2 rounded-full" />
              <span class="text-default-500"
                >原作品 #{{ preview.target_id }}</span
              >
            </p>
            <p class="font-semibold">{{ preview.target_name }}</p>
            <p v-if="preview.vndb_anchor" class="text-default-500 text-xs">
              VNDB {{ preview.vndb_anchor }} 仍然对应原作品
            </p>
            <div>
              <p class="text-default-600 mb-1 text-xs font-medium">
                留下 {{ staying.length }} 个发行
              </p>
              <ul class="space-y-0.5 text-sm">
                <li v-for="r in stayingShown" :key="r.id" class="truncate">
                  <span class="text-default-500 tabular-nums">
                    {{ releaseDateText(r) }}
                  </span>
                  · {{ r.title || '（无标题）' }}
                </li>
              </ul>
              <button
                v-if="staying.length > PREVIEW_LIMIT"
                type="button"
                class="text-primary mt-1 text-xs"
                @click="showAllStaying = !showAllStaying"
              >
                {{
                  showAllStaying
                    ? '收起'
                    : `还有 ${staying.length - PREVIEW_LIMIT} 个`
                }}
              </button>
            </div>
            <p v-if="tombstones.length" class="text-default-500 text-xs">
              另留 {{ tombstones.length }} 条「已移走」记录，VNDB
              导入不会把这些发行建回来
            </p>
            <dl
              class="text-default-500 grid grid-cols-2 gap-x-4 gap-y-1 text-xs"
            >
              <template
                v-for="key in ['credits', 'characters', 'tags', 'titles']"
                :key="key"
              >
                <dt>{{ SPLIT_COUNT_LABELS[key] }}留在原作品</dt>
                <dd class="text-right tabular-nums">
                  {{ countOf(preview.stays, key) }}
                </dd>
              </template>
            </dl>
          </div>

          <div
            class="border-primary-200 bg-primary-50/40 space-y-3 rounded-xl border p-4"
          >
            <p class="flex items-center gap-2 text-xs">
              <span class="bg-warning-500 size-2 rounded-full" />
              <span class="text-default-500">新作品（新编号）</span>
            </p>
            <p
              :class="
                cn('font-semibold', !draft.displayName && 'text-default-400')
              "
            >
              {{ draft.displayName || '还没有显示名' }}
            </p>
            <p class="text-default-500 text-xs">
              原语言：{{
                EDIT_LANGUAGE_LABELS[draft.olang] ?? (draft.olang || '未选')
              }}
            </p>
            <div v-if="draft.titles.length">
              <p class="text-default-600 mb-1 text-xs font-medium">标题</p>
              <ul class="space-y-0.5 text-sm">
                <li v-for="(t, i) in draft.titles" :key="i" class="truncate">
                  {{ t.title || '（空）' }}
                  <span class="text-default-400 text-xs">
                    {{ EDIT_LANGUAGE_LABELS[t.lang] ?? (t.lang || '无语言') }}
                  </span>
                </li>
              </ul>
            </div>
            <div>
              <p class="text-default-600 mb-1 text-xs font-medium">
                移入 {{ leaving.length }} 个发行
              </p>
              <ul class="space-y-0.5 text-sm">
                <li v-for="r in leaving" :key="r.id" class="truncate">
                  <span class="text-default-500 tabular-nums">
                    {{ releaseDateText(r) }}
                  </span>
                  · {{ r.title || '（无标题）' }}
                </li>
              </ul>
            </div>
            <div>
              <p class="text-default-600 mb-1 text-xs font-medium">正式对应</p>
              <p v-if="!promoted.length" class="text-default-400 text-sm">
                没有：标题、标签、制作人员、角色都要人工补
              </p>
              <ul v-else class="space-y-0.5 text-sm">
                <li v-for="ref in promoted" :key="ref.source + ref.external_id">
                  {{ SPLIT_SOURCE_LABELS[ref.source] ?? ref.source }}
                  {{ ref.external_id }}
                  <span class="text-default-400 text-xs">
                    · 下次导入补齐资料
                  </span>
                </li>
              </ul>
            </div>
            <dl
              class="text-default-500 grid grid-cols-2 gap-x-4 gap-y-1 text-xs"
            >
              <template
                v-for="key in ['release_labels', 'credits', 'release_refs']"
                :key="key"
              >
                <dt>一起带走的{{ SPLIT_COUNT_LABELS[key] }}</dt>
                <dd class="text-right tabular-nums">
                  {{ countOf(preview.moves, key) }}
                </dd>
              </template>
            </dl>
          </div>

          <p class="text-default-500 text-xs leading-relaxed">
            下游各站的资源、评论仍挂在原作品上；站点会收到拆分通知，自行决定是否搬到新作品。
          </p>
        </template>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import type {
  CatalogProposalList,
  CatalogRevisionList
} from '#shared/types/catalog'

useHead({ title: '工作台 · 未萌编辑台' })

const api = useEditorApi()
const { session, isReviewer } = useEditorSession()
const proposalNames = useEntityNames()
const revisionNames = useEntityNames()

const { data: openProposals, pending: proposalsPending } = await useAsyncData(
  'dashboard-open-proposals',
  () =>
    api<CatalogProposalList>('/v2/me/proposals', {
      query: { state: 'open', limit: 5, include_total: true }
    }).catch(() => null)
)

const { data: mergedProposals } = await useAsyncData(
  'dashboard-merged-proposals',
  () =>
    api<CatalogProposalList>('/v2/me/proposals', {
      query: { state: 'merged', limit: 1, include_total: true }
    }).catch(() => null)
)

const { data: reviewQueue } = await useAsyncData(
  'dashboard-review-queue',
  () =>
    isReviewer.value
      ? api<CatalogProposalList>('/v2/moderation/proposals', {
          query: { limit: 1, include_total: true }
        }).catch(() => null)
      : Promise.resolve(null)
)

const { data: recent, pending: recentPending } = await useAsyncData(
  'dashboard-recent',
  () =>
    api<CatalogRevisionList>('/v2/catalog/revisions', {
      query: { limit: 8 }
    }).catch(() => null)
)

watch(
  [openProposals, recent],
  ([proposals, revisions]) => {
    proposalNames.resolve(proposals?.items ?? [])
    revisionNames.resolve(revisions?.items ?? [])
  },
  { immediate: true }
)

const greeting = computed(() => {
  const hour = new Date().getHours()
  if (hour < 6) return '夜深了'
  if (hour < 12) return '早上好'
  if (hour < 18) return '下午好'
  return '晚上好'
})

const stats = computed(() => [
  {
    label: '待审中的提案',
    value: openProposals.value?.total ?? 0,
    icon: 'lucide:clock',
    to: '/me/proposals?state=open'
  },
  {
    label: '已合并的提案',
    value: mergedProposals.value?.total ?? 0,
    icon: 'lucide:git-merge',
    to: '/me/proposals?state=merged'
  },
  ...(isReviewer.value
    ? [
        {
          label: '等待审核',
          value: reviewQueue.value?.total ?? 0,
          icon: 'lucide:inbox',
          to: '/review'
        }
      ]
    : [])
])
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <KunHeader
      :name="`${greeting}，${session?.name ?? ''}`"
      description="这里是 NextMoe 资料库的编辑台：查找条目、提交修改、跟进你的提案。"
    />

    <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
      <NuxtLink
        v-for="stat in stats"
        :key="stat.label"
        :to="stat.to"
        class="border-default-200 bg-content1 hover:border-primary-200 flex items-center gap-4 rounded-xl border p-4 transition-colors"
      >
        <span
          class="bg-primary-50 text-primary flex size-10 items-center justify-center rounded-lg"
        >
          <KunIcon :name="stat.icon" class="size-5" />
        </span>
        <div>
          <p class="text-2xl font-semibold tabular-nums">{{ stat.value }}</p>
          <p class="text-default-500 text-sm">{{ stat.label }}</p>
        </div>
      </NuxtLink>
    </div>

    <div class="grid gap-6 lg:grid-cols-2">
      <KunCard bordered padding="md">
        <div class="mb-2 flex items-center justify-between">
          <h2 class="font-semibold">我的待审提案</h2>
          <NuxtLink to="/me/proposals" class="text-primary text-sm"
            >全部</NuxtLink
          >
        </div>
        <KunLoading v-if="proposalsPending" spinner />
        <ul
          v-else-if="openProposals?.items?.length"
          class="divide-default-200 divide-y"
        >
          <CommonProposalItem
            v-for="proposal in openProposals.items"
            :key="proposal.id"
            :proposal="proposal"
            :name="proposalNames.nameOf(proposal)"
          />
        </ul>
        <KunNull
          v-else
          description="没有待审中的提案"
          :is-show-sticker="false"
        />
      </KunCard>

      <KunCard bordered padding="md">
        <div class="mb-2 flex items-center justify-between">
          <h2 class="font-semibold">全站最近修订</h2>
          <NuxtLink to="/changes" class="text-primary text-sm">全部</NuxtLink>
        </div>
        <KunLoading v-if="recentPending" spinner />
        <ul
          v-else-if="recent?.items?.length"
          class="divide-default-200 divide-y"
        >
          <CommonRevisionItem
            v-for="revision in recent.items"
            :key="revision.id"
            :revision="revision"
            :name="revisionNames.nameOf(revision)"
          />
        </ul>
        <KunNull v-else description="暂无修订记录" :is-show-sticker="false" />
      </KunCard>
    </div>
  </div>
</template>

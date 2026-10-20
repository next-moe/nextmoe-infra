<script setup lang="ts">
import type {
  ActivityList,
  ActivityProposal,
  ActivityRevision
} from '#shared/types/activity'

useHead({ title: '工作台 · 未萌编辑台' })

const api = useEditorApi()
const { session, isReviewer } = useEditorSession()
const proposalNames = useEntityNames()
const revisionNames = useEntityNames()

const { data: quota } = await useEditQuota()

const { data: openProposals, pending: proposalsPending } = await useAsyncData(
  'dashboard-open-proposals',
  () =>
    api<ActivityList<ActivityProposal>>('/v2/me/proposals', {
      query: { state: 'open', limit: 5, include_total: true }
    }).catch(() => null)
)

const { data: mergedProposals } = await useAsyncData(
  'dashboard-merged-proposals',
  () =>
    api<ActivityList<ActivityProposal>>('/v2/me/proposals', {
      query: { state: 'merged', limit: 1, include_total: true }
    }).catch(() => null)
)

const { data: recent, pending: recentPending } = await useAsyncData(
  'dashboard-recent',
  () =>
    api<ActivityList<ActivityRevision>>('/v2/catalog/revisions', {
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

const canApply = computed(() => quota.value?.quota_tier === 'user')

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
  }
])
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <KunHeader
      :name="`${greeting}，${session?.name ?? ''}`"
      description="这里是 NextMoe 资料库的编辑台：查找条目、提交修改、跟进你的提案。"
    />

    <DashboardApplyCallout v-if="canApply" />

    <div class="grid grid-cols-1 items-start gap-6 lg:grid-cols-3">
      <div class="lg:col-span-1">
        <DashboardQuotaCard :quota="quota ?? null" />
      </div>

      <div
        class="grid min-w-0 grid-cols-1 content-start gap-3 sm:grid-cols-2 lg:col-span-2"
      >
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

        <div v-if="isReviewer" class="sm:col-span-2">
          <DashboardReviewerCard />
        </div>
      </div>
    </div>

    <DashboardQuickLinks :is-reviewer="isReviewer" :can-apply="canApply" />

    <div class="grid grid-cols-1 items-start gap-6 lg:grid-cols-2">
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
            :to="`/me/proposals/${proposal.id}`"
          />
        </ul>
        <KunNull
          v-else
          description="没有待审中的提案"
          :is-show-sticker="false"
        />
      </KunCard>

      <DashboardDecidedProposals />
    </div>

    <KunCard bordered padding="md">
      <div class="mb-2 flex items-center justify-between">
        <h2 class="font-semibold">全站最近修订</h2>
        <NuxtLink to="/changes" class="text-primary text-sm">全部</NuxtLink>
      </div>
      <KunLoading v-if="recentPending" spinner />
      <ul v-else-if="recent?.items?.length" class="divide-default-200 divide-y">
        <DashboardRecentChange
          v-for="revision in recent.items"
          :key="revision.id"
          :revision="revision"
          :name="revisionNames.nameOf(revision)"
        />
      </ul>
      <KunNull v-else description="暂无修订记录" :is-show-sticker="false" />
    </KunCard>
  </div>
</template>

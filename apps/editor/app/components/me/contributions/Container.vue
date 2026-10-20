<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { ActivityList } from '#shared/types/activity'
import type { CatalogProposal, CatalogRevision } from '#shared/types/catalog'
import {
  ACTIVITY_WEEKS,
  CONTRIBUTION_FAMILIES,
  CONTRIBUTION_STATES
} from '~/constants/activity'
import { PROPOSAL_STATE } from '~/constants/catalog'

useHead({ title: '我的贡献 · 未萌编辑台' })

const api = useEditorApi()
const { session } = useEditorSession()
const names = useEntityNames()

const totalOf = (query: Record<string, string | number | boolean>) =>
  api<ActivityList<CatalogProposal>>('/v2/me/proposals', {
    query: { ...query, limit: 1, include_total: true }
  })
    .then((list) => list.total ?? 0)
    .catch(() => null)

const { data: stateTotals, pending: statesPending } = await useAsyncData(
  'contributions-states',
  async () =>
    Object.fromEntries(
      await Promise.all(
        CONTRIBUTION_STATES.map(
          async (state) => [state, await totalOf({ state })] as const
        )
      )
    )
)

const { data: familyTotals } = await useAsyncData(
  'contributions-families',
  async () =>
    (
      await Promise.all(
        CONTRIBUTION_FAMILIES.map(async (family) => ({
          ...family,
          count: (await totalOf({ object: family.key })) ?? 0
        }))
      )
    )
      .filter((family) => family.count > 0)
      .sort((a, b) => b.count - a.count)
)

const { data: recentProposals } = await useAsyncData(
  'contributions-recent-proposals',
  () =>
    api<ActivityList<CatalogProposal>>('/v2/me/proposals', {
      query: { limit: 100 }
    })
      .then((list) => list.items ?? [])
      .catch(() => [] as CatalogProposal[])
)

const { data: revisions, pending: revisionsPending } = await useAsyncData(
  'contributions-revisions',
  () =>
    session.value
      ? api<ActivityList<CatalogRevision>>('/v2/catalog/revisions', {
          query: { actor_uid: session.value.id, limit: 20, include_total: true }
        }).catch(() => null)
      : Promise.resolve(null)
)

watch(revisions, (list) => names.resolve(list?.items ?? []), {
  immediate: true
})

const totalProposals = computed(() =>
  Object.values(stateTotals.value ?? {}).reduce<number>(
    (sum, value) => sum + (value ?? 0),
    0
  )
)

const mergeRate = computed(() => {
  const merged = stateTotals.value?.merged ?? 0
  const declined = stateTotals.value?.declined ?? 0
  const decided = merged + declined
  return decided ? Math.round((merged / decided) * 100) : null
})

const weeks = computed(() =>
  weeklyActivity(
    (recentProposals.value ?? []).map((proposal) => proposal.created_at),
    ACTIVITY_WEEKS
  )
)
const busiestWeek = computed(() =>
  Math.max(1, ...weeks.value.map((week) => week.count))
)
const familyMax = computed(() =>
  Math.max(1, ...(familyTotals.value ?? []).map((family) => family.count))
)
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-6">
    <KunHeader name="我的贡献" description="你提交过的提案与已经生效的修订。" />

    <KunLoading v-if="statesPending" spinner />
    <div v-else class="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
      <div class="border-default-200 bg-content1 rounded-xl border p-4">
        <p class="text-2xl font-semibold tabular-nums">{{ totalProposals }}</p>
        <p class="text-default-500 text-sm">提案总数</p>
      </div>
      <NuxtLink
        v-for="state in CONTRIBUTION_STATES"
        :key="state"
        :to="`/me/proposals?state=${state}`"
        class="border-default-200 bg-content1 hover:border-primary-200 rounded-xl border p-4 transition-colors"
      >
        <p class="text-2xl font-semibold tabular-nums">
          {{ stateTotals?.[state] ?? '—' }}
        </p>
        <p class="text-default-500 text-sm">
          {{ PROPOSAL_STATE[state]?.label ?? state }}
        </p>
      </NuxtLink>
    </div>

    <div class="grid grid-cols-1 gap-6 lg:grid-cols-3">
      <KunCard bordered padding="md" class-name="lg:col-span-2">
        <div class="mb-3 flex items-baseline justify-between gap-2">
          <h2 class="font-semibold">近 {{ ACTIVITY_WEEKS }} 周提交</h2>
          <span class="text-default-400 text-xs">按最近 100 条提案统计</span>
        </div>
        <div
          class="flex h-32 items-end gap-1.5"
          role="img"
          :aria-label="`近 ${ACTIVITY_WEEKS} 周每周提交的提案数`"
        >
          <div
            v-for="week in weeks"
            :key="week.start"
            class="flex h-full flex-1 flex-col justify-end"
            :title="`${formatDateTime(week.start).slice(0, 10)} 起一周：${week.count} 条`"
          >
            <div
              :class="
                cn(
                  'rounded-t-sm',
                  week.count ? 'bg-primary-400' : 'bg-default-200'
                )
              "
              :style="{
                height: week.count
                  ? `${Math.max((week.count / busiestWeek) * 100, 6)}%`
                  : '4px'
              }"
            />
          </div>
        </div>
        <div class="text-default-400 mt-1 flex justify-between text-xs">
          <span>{{ formatDateTime(weeks[0]?.start).slice(5, 10) }}</span>
          <span>本周</span>
        </div>
      </KunCard>

      <KunCard bordered padding="md">
        <h2 class="mb-3 font-semibold">按条目类型</h2>
        <ul v-if="familyTotals?.length" class="space-y-2">
          <li v-for="family in familyTotals" :key="family.key" class="text-sm">
            <div class="mb-1 flex justify-between">
              <span>{{ family.label }}</span>
              <span class="text-default-500 tabular-nums">
                {{ family.count }}
              </span>
            </div>
            <div class="bg-default-100 h-1.5 rounded-full">
              <div
                class="bg-primary-400 h-1.5 rounded-full"
                :style="{ width: `${(family.count / familyMax) * 100}%` }"
              />
            </div>
          </li>
        </ul>
        <p v-else class="text-default-400 text-sm">还没有提案</p>
        <p
          v-if="mergeRate !== null"
          class="border-default-200 text-default-500 mt-4 border-t pt-3 text-sm"
        >
          已处理的提案中有
          <span class="text-foreground font-medium">{{ mergeRate }}%</span>
          被合并
        </p>
      </KunCard>
    </div>

    <KunCard bordered padding="md">
      <div class="mb-2 flex items-baseline justify-between gap-2">
        <h2 class="font-semibold">已生效的修订</h2>
        <span
          v-if="revisions?.total !== undefined"
          class="text-default-400 text-xs"
        >
          共 {{ revisions.total }} 次
        </span>
      </div>
      <KunLoading v-if="revisionsPending" spinner />
      <ul
        v-else-if="revisions?.items?.length"
        class="divide-default-200 divide-y"
      >
        <DashboardRecentChange
          v-for="revision in revisions.items"
          :key="revision.id"
          :revision="revision"
          :name="names.nameOf(revision)"
        />
      </ul>
      <KunNull v-else description="还没有生效的修订" :is-show-sticker="false" />
    </KunCard>
  </div>
</template>

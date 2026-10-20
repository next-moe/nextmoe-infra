<script setup lang="ts">
import { GUIDELINE_SECTIONS } from '~/constants/guidelines'
import { QUOTA_TIER_LABELS } from '~/constants/quota'

useHead({ title: '编辑规范 · 未萌编辑台' })

const { data: quota, pending: quotaPending } = useEditQuota()
</script>

<template>
  <div class="mx-auto flex max-w-6xl gap-8">
    <nav
      class="sticky top-16 hidden h-fit w-48 shrink-0 lg:block"
      aria-label="编辑规范目录"
    >
      <p class="text-default-400 mb-2 px-2 text-xs font-medium">目录</p>
      <ul class="space-y-0.5 text-sm">
        <li v-for="section in GUIDELINE_SECTIONS" :key="section.id">
          <a
            :href="`#${section.id}`"
            class="text-default-600 hover:bg-default-100 hover:text-foreground block rounded-md px-2 py-1 transition-colors"
          >
            {{ section.title }}
          </a>
        </li>
        <li>
          <a
            href="#quota"
            class="text-default-600 hover:bg-default-100 hover:text-foreground block rounded-md px-2 py-1 transition-colors"
          >
            我的配额
          </a>
        </li>
      </ul>
    </nav>

    <div class="min-w-0 flex-1 space-y-4">
      <KunHeader
        name="编辑规范"
        description="动手之前花几分钟读一读，能让你的提案更快通过审核。"
      />

      <KunCard
        v-for="section in GUIDELINE_SECTIONS"
        :id="section.id"
        :key="section.id"
        bordered
        padding="md"
        class-name="scroll-mt-16"
      >
        <h2 class="mb-3 flex items-center gap-2 font-semibold">
          <KunIcon :name="section.icon" class="text-primary size-4" />
          {{ section.title }}
        </h2>
        <ul class="text-default-700 space-y-2 text-sm leading-relaxed">
          <li
            v-for="(point, index) in section.points"
            :key="index"
            class="flex gap-2"
          >
            <span class="bg-default-300 mt-2 size-1.5 shrink-0 rounded-full" />
            <span>{{ point }}</span>
          </li>
        </ul>
      </KunCard>

      <KunCard id="quota" bordered padding="md" class-name="scroll-mt-16">
        <h2 class="mb-3 flex items-center gap-2 font-semibold">
          <KunIcon name="lucide:gauge" class="text-primary size-4" />
          我的配额
        </h2>
        <div v-if="quotaPending && !quota" class="space-y-2" aria-busy="true">
          <KunSkeleton variant="text" class-name="text-sm" width="70%" />
          <KunSkeleton height="4rem" rounded="lg" />
        </div>
        <p v-else-if="!quota" class="text-default-400 text-sm">
          配额信息暂不可用
        </p>
        <template v-else>
          <p class="text-default-600 mb-3 text-sm">
            你当前的身份是「{{
              labelOf(QUOTA_TIER_LABELS, quota.quota_tier)
            }}」，以下额度按最近
            {{ quota.window_hours }} 小时滚动计算，平台可能随时调整。
          </p>
          <dl class="grid gap-2 sm:grid-cols-3">
            <div class="bg-default-50 rounded-lg p-3">
              <dt class="text-default-500 text-xs">每日提案</dt>
              <dd class="tabular-nums">
                {{ quotaCounterLabel(quota.proposals) }}
              </dd>
            </div>
            <div class="bg-default-50 rounded-lg p-3">
              <dt class="text-default-500 text-xs">免审生效</dt>
              <dd class="tabular-nums">
                {{ quotaCounterLabel(quota.direct_edits) }}
              </dd>
            </div>
            <div class="bg-default-50 rounded-lg p-3">
              <dt class="text-default-500 text-xs">同时待审</dt>
              <dd class="tabular-nums">
                {{ quotaCounterLabel(quota.open_proposals) }}
              </dd>
            </div>
          </dl>
          <p
            v-if="quota.quota_tier === 'user'"
            class="text-default-500 mt-3 text-sm"
          >
            经常编辑的话，可以
            <NuxtLink to="/apply" class="text-primary">申请成为编辑者</NuxtLink>
            ，让修改在额度内直接生效。
          </p>
        </template>
      </KunCard>
    </div>
  </div>
</template>

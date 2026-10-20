<script setup lang="ts">
import { QUOTA_TIER_LABELS } from '~/constants/quota'

useHead({ title: '申请成为编辑者 · 未萌编辑台' })

const { data: quota } = await useEditQuota()
const { application, loaded, loadError, load } = useEditorApplication()

onMounted(load)

const tier = computed(() => quota.value?.quota_tier ?? 'user')
const alreadyEditing = computed(() => tier.value !== 'user')
const showForm = computed(
  () =>
    loaded.value &&
    !alreadyEditing.value &&
    !loadError.value &&
    application.value?.status !== 'pending' &&
    application.value?.status !== 'approved'
)
</script>

<template>
  <div class="mx-auto max-w-3xl space-y-6">
    <KunHeader
      name="申请成为编辑者"
      description="编辑者的修改无需等待审核即可直接生效，适合长期、稳定地维护资料的贡献者。"
    />

    <KunCard bordered padding="md">
      <h2 class="mb-3 font-semibold">编辑者可以做什么</h2>
      <ul class="text-default-600 space-y-2 text-sm">
        <li class="flex gap-2">
          <KunIcon
            name="lucide:circle-check"
            class="text-success mt-0.5 size-4 shrink-0"
          />
          作品、发行等条目的修改提交后直接生效，不必排队等待审核；每天有免审额度，用完后的修改会照常进入审核。
        </li>
        <li class="flex gap-2">
          <KunIcon
            name="lucide:circle-check"
            class="text-success mt-0.5 size-4 shrink-0"
          />
          更高的每日提案上限和同时待审上限。
        </li>
        <li class="flex gap-2">
          <KunIcon
            name="lucide:info"
            class="text-default-400 mt-0.5 size-4 shrink-0"
          />
          角色、人物、标签等被多部作品共用的条目，修改默认仍需审核。
        </li>
        <li class="flex gap-2">
          <KunIcon
            name="lucide:info"
            class="text-default-400 mt-0.5 size-4 shrink-0"
          />
          编辑者不能审核其他人的提案；审核由版主、管理员负责。
        </li>
      </ul>

      <div v-if="quota" class="border-default-200 mt-4 border-t pt-3 text-sm">
        <p class="text-default-500 mb-2">
          你目前是「{{ labelOf(QUOTA_TIER_LABELS, quota.quota_tier) }}」，最近
          {{ quota.window_hours }} 小时内的额度：
        </p>
        <dl class="grid grid-cols-3 gap-2">
          <div class="bg-default-50 rounded-lg p-2">
            <dt class="text-default-500 text-xs">提案</dt>
            <dd class="tabular-nums">
              {{ quotaCounterLabel(quota.proposals) }}
            </dd>
          </div>
          <div class="bg-default-50 rounded-lg p-2">
            <dt class="text-default-500 text-xs">免审生效</dt>
            <dd class="tabular-nums">
              {{ quotaCounterLabel(quota.direct_edits) }}
            </dd>
          </div>
          <div class="bg-default-50 rounded-lg p-2">
            <dt class="text-default-500 text-xs">同时待审</dt>
            <dd class="tabular-nums">
              {{ quotaCounterLabel(quota.open_proposals) }}
            </dd>
          </div>
        </dl>
      </div>
    </KunCard>

    <KunInfo
      v-if="alreadyEditing"
      color="success"
      variant="flat"
      :title="`你已经是${labelOf(QUOTA_TIER_LABELS, tier)}`"
      description="无需再申请。修改时留意每日免审额度即可。"
    />

    <KunLoading v-else-if="!loaded" spinner />

    <KunInfo
      v-else-if="loadError"
      color="danger"
      variant="flat"
      title="暂时无法读取申请状态"
      :description="loadError"
    />

    <template v-else>
      <ApplyStatus
        v-if="application"
        :application="application"
        :role-active="alreadyEditing"
      />
      <ApplyForm v-if="showForm" :again="application?.status === 'declined'" />
    </template>

    <p class="text-default-400 text-sm">
      申请前建议先读一读
      <NuxtLink to="/guidelines" class="text-primary">编辑规范</NuxtLink>
      ，并用普通提案积累一些被合并的修改。
    </p>
  </div>
</template>

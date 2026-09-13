<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import {
  DEV_POLICY_MODE_ACTIVE_CLASS,
  DEV_POLICY_MODE_HINTS,
  DEV_POLICY_MODE_ICON_CLASS,
  DEV_POLICY_MODE_LABELS,
} from '~/constants/devapi'
import type {
  DevPolicy,
  DevPolicyMatrix,
  DevPolicyMode,
} from '~~/shared/types/devapi'

const api = useApi()

const { data, status, refresh, error } = await useApiFetch<DevPolicyMatrix>(
  '/admin/devapi/policies'
)

const policies = computed(() => data.value?.policies ?? [])
const editable = computed(() => data.value?.editable ?? false)
const isLoading = computed(() => status.value === 'pending')

// The target outlives the close so the panel still has a policy to render while
// KunModal's leave transition plays; `pendingOpen` drives the dialog.
const pending = ref<{ policy: DevPolicy; mode: DevPolicyMode } | null>(null)
const pendingOpen = ref(false)
const submitting = ref(false)

const askSet = (policy: DevPolicy, mode: DevPolicyMode) => {
  if (!editable.value || policy.mode === mode) return
  pending.value = { policy, mode }
  pendingOpen.value = true
}

const confirmSet = async () => {
  const target = pending.value
  if (!target) return
  submitting.value = true
  try {
    const res =
      target.mode === target.policy.default
        ? await api.delete(`/admin/devapi/policies/${target.policy.key}`)
        : await api.put(`/admin/devapi/policies/${target.policy.key}`, {
            mode: target.mode,
          })
    if (res.code === 0) {
      useKunMessage('平台策略已更新', 'success')
      pendingOpen.value = false
      await refresh()
    } else {
      useKunMessage(res.message || '更新失败', 'error')
    }
  } finally {
    submitting.value = false
  }
}

const cellClass = (policy: DevPolicy, mode: DevPolicyMode) =>
  cn(
    'rounded-lg border px-3 py-2 text-left transition-colors',
    policy.mode === mode
      ? DEV_POLICY_MODE_ACTIVE_CLASS[mode]
      : 'border-default-200 bg-content1',
    editable.value && policy.mode !== mode
      ? 'cursor-pointer hover:border-primary'
      : 'cursor-default'
  )
</script>

<template>
  <KunCard content-class="space-y-4 p-6 items-stretch">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h2 class="text-lg font-bold text-foreground">平台策略矩阵</h2>
        <p class="mt-1 text-sm text-default-500">
          决定开发者在门户里能自助做到哪一步。未设置的行走代码默认值，改回默认即删除该行。
        </p>
      </div>
      <KunChip v-if="!editable" color="default" variant="flat" size="sm">
        只读 · 仅 ren 可修改
      </KunChip>
    </div>

    <CommonFetchError v-if="error" @retry="refresh" />

    <div v-else-if="isLoading" class="flex items-center justify-center py-10">
      <KunIcon
        name="lucide:loader-circle"
        class="size-6 animate-spin text-primary"
      />
    </div>

    <div v-else class="space-y-3">
      <div
        v-for="policy in policies"
        :key="policy.key"
        class="grid gap-3 border-t border-default-200 pt-3 first:border-t-0 first:pt-0 md:grid-cols-[16rem_1fr]"
      >
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <span class="font-medium text-foreground">
              {{ policy.label_zh }}
            </span>
            <KunChip v-if="!policy.overridden" color="default" variant="flat" size="xs">
              默认
            </KunChip>
          </div>
          <p class="mt-0.5 font-mono text-xs text-default-400">
            {{ policy.key }}
          </p>
          <p class="mt-1 text-sm text-default-500">{{ policy.desc_zh }}</p>
        </div>

        <div class="flex flex-wrap gap-2">
          <button
            v-for="mode in policy.modes"
            :key="mode"
            type="button"
            :disabled="!editable"
            :class="cellClass(policy, mode)"
            @click="askSet(policy, mode)"
          >
            <span class="flex items-center gap-1.5">
              <KunIcon
                v-if="policy.mode === mode"
                name="lucide:check"
                class="size-4"
                :class="DEV_POLICY_MODE_ICON_CLASS[mode]"
              />
              <span class="text-sm font-medium text-foreground">
                {{ DEV_POLICY_MODE_LABELS[mode] ?? mode }}
              </span>
              <KunChip
                v-if="policy.default === mode"
                color="default"
                variant="flat"
                size="xs"
              >
                默认
              </KunChip>
            </span>
            <span class="mt-0.5 block text-xs text-default-400">
              {{ DEV_POLICY_MODE_HINTS[mode] }}
            </span>
          </button>
        </div>
      </div>
    </div>

    <KunModal
      v-model="pendingOpen"
      size="md"
      role="alertdialog"
      aria-label="修改平台策略"
    >
      <div v-if="pending" class="space-y-4">
        <h2 class="text-xl font-bold text-foreground">修改平台策略</h2>
        <p class="text-sm text-default-500">
          将「{{ pending.policy.label_zh }}」从
          <span class="font-medium text-foreground">
            {{ DEV_POLICY_MODE_LABELS[pending.policy.mode] }}
          </span>
          改为
          <span class="font-medium text-foreground">
            {{ DEV_POLICY_MODE_LABELS[pending.mode] }}
          </span>
          ，对全部开发者立即生效。
        </p>
        <p class="rounded-lg bg-default-50 p-3 text-sm text-default-500">
          {{ DEV_POLICY_MODE_HINTS[pending.mode] }}
        </p>
        <div class="flex justify-end gap-3">
          <KunButton
            color="default"
            variant="flat"
            :disabled="submitting"
            @click="pendingOpen = false"
          >
            取消
          </KunButton>
          <KunButton color="primary" :disabled="submitting" @click="confirmSet">
            <KunIcon
              v-if="submitting"
              name="lucide:loader-circle"
              class="mr-2 size-4 animate-spin"
            />
            确认
          </KunButton>
        </div>
      </div>
    </KunModal>
  </KunCard>
</template>

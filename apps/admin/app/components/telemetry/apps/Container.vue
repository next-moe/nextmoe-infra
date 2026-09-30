<script setup lang="ts">
import type { TelemetryApp } from '~~/shared/types/telemetry'

const { data, status, error, refresh } = await useApiFetch<TelemetryApp[]>(
  '/admin/telemetry/apps',
  { key: 'telemetry-apps' },
  'telemetry'
)

const apps = computed(() => data.value ?? [])
const isLoading = computed(() => status.value === 'pending')

const createOpen = ref(false)
const tokenOpen = ref(false)
const tokenValue = ref('')

const onCreated = async () => {
  createOpen.value = false
  await refresh()
}

const onToken = (token: string) => {
  tokenValue.value = token
  tokenOpen.value = true
}

const onChanged = () => refresh()
</script>

<template>
  <div class="space-y-5">
    <div
      class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"
    >
      <h1 class="text-foreground text-2xl font-bold">应用与密钥</h1>
      <KunButton
        color="primary"
        class-name="shrink-0 self-start"
        @click="createOpen = true"
      >
        <KunIcon name="lucide:plus" class="mr-2 size-4" />
        新建应用
      </KunButton>
    </div>

    <CommonFetchError v-if="error" :message="error.message" @retry="refresh" />

    <div v-else-if="isLoading" class="flex justify-center py-12">
      <KunIcon
        name="lucide:loader-circle"
        class="text-primary size-8 animate-spin"
      />
    </div>

    <KunCard
      v-else-if="!apps.length"
      content-class="justify-start gap-0"
      class-name="py-12 text-center"
    >
      <p class="text-default-400">还没有监测应用，点击「新建应用」创建。</p>
    </KunCard>

    <div v-else class="space-y-4">
      <TelemetryAppsCard
        v-for="app in apps"
        :key="app.id"
        :app="app"
        @changed="onChanged"
        @token="onToken"
      />
    </div>

    <TelemetryAppsCreateModal v-model="createOpen" @saved="onCreated" />
    <TelemetryAppsTokenModal v-model="tokenOpen" :token="tokenValue" />
  </div>
</template>

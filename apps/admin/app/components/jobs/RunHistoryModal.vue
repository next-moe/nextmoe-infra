<script setup lang="ts">
import { jobStatusMeta, jobTriggerLabel } from '~/constants/jobs'
import type { JobRun } from '~~/shared/types/jobs'

const open = defineModel<boolean>('open', { required: true })
const props = defineProps<{ jobName: string | null }>()

const api = useApi()
const runs = ref<JobRun[]>([])
const loading = ref(false)
const errored = ref(false)

const load = async () => {
  if (!props.jobName) return
  loading.value = true
  errored.value = false
  try {
    const res = await api.get<JobRun[]>(`/admin/jobs/${props.jobName}/runs`, {
      limit: 30
    })
    if (res.code === 0) {
      runs.value = res.data ?? []
    } else {
      errored.value = true
      useKunMessage(res.message || '加载运行历史失败', 'error')
    }
  } finally {
    loading.value = false
  }
}

watch(open, (v) => {
  if (v) {
    runs.value = []
    load()
  }
})

const fmt = (s?: string | null) => (s ? new Date(s).toLocaleString('zh-CN') : '—')
</script>

<template>
  <KunModal v-model="open" size="lg" aria-label="运行历史">
    <div class="space-y-4">
      <h2 class="text-foreground text-xl font-bold">
        运行历史
        <span class="text-default-400 font-mono text-sm">{{ jobName }}</span>
      </h2>

      <div v-if="loading" class="flex justify-center py-8">
        <KunIcon
          name="lucide:loader-circle"
          class="text-primary size-6 animate-spin"
        />
      </div>
      <CommonFetchError v-else-if="errored" @retry="load" />
      <div
        v-else-if="!runs.length"
        class="text-default-400 py-8 text-center text-sm"
      >
        暂无运行记录
      </div>
      <div v-else class="max-h-[60vh] overflow-y-auto">
        <table class="w-full text-sm">
          <thead class="text-default-500">
            <tr>
              <th class="px-2 py-2 text-left font-medium">状态</th>
              <th class="px-2 py-2 text-left font-medium">触发</th>
              <th class="px-2 py-2 text-left font-medium">开始时间</th>
              <th class="px-2 py-2 text-left font-medium">结果</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="run in runs"
              :key="run.id"
              class="border-default-200 border-t align-top"
            >
              <td class="px-2 py-2">
                <KunChip
                  :color="jobStatusMeta(run.status).color"
                  variant="flat"
                  size="xs"
                >
                  {{ jobStatusMeta(run.status).label }}
                </KunChip>
              </td>
              <td class="text-default-500 px-2 py-2">
                {{ jobTriggerLabel(run.trigger) }}
              </td>
              <td class="text-default-500 px-2 py-2 whitespace-nowrap">
                {{ fmt(run.started_at) }}
              </td>
              <td class="px-2 py-2">
                <span v-if="run.error" class="text-danger-600 break-words">
                  {{ run.error }}
                </span>
                <span
                  v-else-if="run.summary"
                  class="text-default-400 font-mono text-xs break-all"
                >
                  {{ JSON.stringify(run.summary) }}
                </span>
                <span v-else class="text-default-300">—</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="flex justify-end">
        <KunButton color="default" variant="flat" @click="open = false">
          关闭
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>

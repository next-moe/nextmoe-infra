<script setup lang="ts">
import type { ArtifactAdminStats } from '~~/shared/types/artifact'

const {
  data: stats,
  status,
  refresh
} = await useApiFetch<ArtifactAdminStats>('/admin/artifact/stats')

const cards = computed(() => {
  const s = stats.value
  return [
    {
      label: '就绪文件',
      value: s ? formatNumber(s.total_count) : '—',
      icon: 'lucide:circle-check',
      color: 'text-success'
    },
    {
      label: '占用空间',
      value: s ? formatFileSize(s.total_bytes) : '—',
      icon: 'lucide:hard-drive',
      color: 'text-primary'
    },
    {
      label: '上传中',
      value: s ? formatNumber(s.uploading) : '—',
      icon: 'lucide:loader',
      color: 'text-warning'
    },
    {
      label: '失败',
      value: s ? formatNumber(s.failed) : '—',
      icon: 'lucide:circle-x',
      color: 'text-danger'
    },
    {
      label: '已软删除',
      value: s ? formatNumber(s.soft_deleted) : '—',
      icon: 'lucide:trash-2',
      color: 'text-default-400'
    }
  ]
})

const siteRows = computed(() =>
  Object.entries(stats.value?.by_site ?? {})
    .map(([site, v]) => ({ site, count: v.count, bytes: v.bytes }))
    .sort((a, b) => b.bytes - a.bytes)
)
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <h1 class="text-foreground text-2xl font-bold">用量概览</h1>
      <KunButton
        variant="flat"
        size="sm"
        :disabled="status === 'pending'"
        @click="refresh()"
      >
        <KunIcon
          :name="status === 'pending' ? 'lucide:loader-circle' : 'lucide:refresh-cw'"
          :class="status === 'pending' ? 'mr-1 size-4 animate-spin' : 'mr-1 size-4'"
        />
        刷新
      </KunButton>
    </div>

    <div class="grid grid-cols-2 gap-4 md:grid-cols-3 lg:grid-cols-5">
      <KunCard
        v-for="c in cards"
        :key="c.label"
        content-class="justify-start gap-0"
        class-name="p-4"
      >
        <div class="text-default-500 flex items-center gap-2 text-sm">
          <KunIcon :name="c.icon" :class="`size-4 ${c.color}`" />
          {{ c.label }}
        </div>
        <div class="text-foreground mt-1 text-2xl font-bold">{{ c.value }}</div>
      </KunCard>
    </div>

    <div>
      <h2 class="text-foreground mb-3 text-lg font-semibold">按站点分布</h2>
      <div
        v-if="siteRows.length"
        class="bg-content1 overflow-x-auto rounded-xl shadow-sm"
      >
        <table class="w-full min-w-[32rem] text-sm">
          <thead class="bg-content2 text-default-500">
            <tr>
              <th class="px-4 py-2 text-left font-medium">站点 (site_key)</th>
              <th class="px-4 py-2 text-right font-medium">就绪文件数</th>
              <th class="px-4 py-2 text-right font-medium">占用空间</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="r in siteRows"
              :key="r.site"
              class="border-default-200 border-t"
            >
              <td class="text-foreground px-4 py-2 font-mono">{{ r.site }}</td>
              <td class="px-4 py-2 text-right">{{ formatNumber(r.count) }}</td>
              <td class="px-4 py-2 text-right">{{ formatFileSize(r.bytes) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <KunCard v-else class-name="p-8">
        <p class="text-default-400 text-center">暂无数据</p>
      </KunCard>
    </div>
  </div>
</template>

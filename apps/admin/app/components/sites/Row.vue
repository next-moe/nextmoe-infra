<script setup lang="ts">
defineProps<{ site: Site }>()
const emit = defineEmits<{
  edit: []
  delete: []
}>()
</script>

<template>
  <KunCard is-hoverable content-class="justify-start gap-0" class-name="p-4">
    <div class="flex items-center gap-4">
      <div class="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary-100">
        <KunIcon name="lucide:globe" class="size-5 text-primary" />
      </div>

      <div class="min-w-0 flex-1">
        <div class="flex flex-wrap items-baseline gap-x-2">
          <h3 class="truncate font-semibold text-foreground">{{ site.name }}</h3>
          <span class="truncate text-sm text-primary">{{ site.domain }}</span>
        </div>
        <p class="truncate text-sm text-default-500">
          {{ site.description || '暂无描述' }}
        </p>
      </div>

      <span class="hidden shrink-0 text-xs text-default-400 lg:block">
        创建于 {{ new Date(site.created_at).toLocaleDateString('zh-CN') }}
      </span>

      <div class="flex shrink-0 gap-1">
        <KunButton
          variant="light"
          size="sm"
          is-icon-only
          aria-label="编辑站点"
          @click="emit('edit')"
        >
          <KunIcon name="lucide:pencil" class="size-5" />
        </KunButton>
        <KunButton
          variant="light"
          color="danger"
          size="sm"
          is-icon-only
          aria-label="删除站点"
          @click="emit('delete')"
        >
          <KunIcon name="lucide:trash-2" class="size-5" />
        </KunButton>
      </div>
    </div>
  </KunCard>
</template>

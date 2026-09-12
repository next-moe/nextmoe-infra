<script setup lang="ts">
import { cn } from '@kungal/ui-core'

import { CATALOG_IMAGE_REF_KIND_LABELS } from '~/constants/catalog'

const props = defineProps<{
  hash: string
  force: boolean
}>()

const emit = defineEmits<{ deleted: [] }>()

const open = defineModel<boolean>({ required: true })

const api = useApi()
const catalogApi = useApi('catalog')

const refs = ref<CatalogImageReferenceItem[]>([])
const refsLoading = ref(false)
const refsFailed = ref(false)
const detachRefs = ref(true)
const deleting = ref(false)

const kindLabel = (kind: string) => CATALOG_IMAGE_REF_KIND_LABELS[kind] ?? kind

const loadReferences = async (hash: string) => {
  refs.value = []
  refsFailed.value = false
  detachRefs.value = true
  if (!hash) return
  refsLoading.value = true
  try {
    const res = await catalogApi.get<CatalogImageReferencesData>(
      '/admin/catalog/image-references',
      { hash }
    )
    if (res.code === 0) {
      refs.value = res.data?.items ?? []
    } else {
      refsFailed.value = true
      useKunMessage(res.message || '引用检查失败', 'error')
    }
  } finally {
    refsLoading.value = false
  }
}

watch(
  () => [open.value, props.hash] as const,
  ([isOpen, hash]) => {
    if (isOpen) loadReferences(hash)
  },
  { immediate: true }
)

const confirmDelete = async () => {
  deleting.value = true
  try {
    if (detachRefs.value && refs.value.length > 0) {
      const detached = await catalogApi.post<CatalogDetachImageReferencesData>(
        '/admin/catalog/image-references/detach',
        { hash: props.hash }
      )
      if (detached.code !== 0) {
        useKunMessage(detached.message || '摘除 catalog 引用失败', 'error')
        return
      }
      useKunMessage(
        `已摘除 ${detached.data?.total_removed ?? 0} 条 catalog 引用`,
        'success'
      )
    }

    const res = await api.delete(
      `/admin/image/${props.hash}${props.force ? '?force=true' : ''}`
    )
    if (res.code === 0) {
      useKunMessage('图片已删除', 'success')
      emit('deleted')
      open.value = false
    } else {
      useKunMessage(res.message || '删除失败', 'error')
    }
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <KunModal v-model="open" :aria-label="force ? '硬删除图片' : '软删除图片'">
    <div class="space-y-4">
      <h2 class="text-xl font-bold text-foreground">
        {{ force ? '硬删除图片' : '软删除图片' }}
      </h2>
      <p
        :class="
          cn(
            'rounded-lg p-3 text-sm',
            force ? 'bg-danger-50 text-danger' : 'bg-warning-50 text-warning-700'
          )
        "
      >
        {{
          force
            ? '物理删除 S3 对象与数据库行，不可恢复。确认继续？'
            : '软删除：30 天后 GC 会物理删除。确认继续？'
        }}
      </p>

      <div
        v-if="refsLoading"
        class="flex items-center gap-2 text-sm text-default-500"
      >
        <KunIcon name="lucide:loader-circle" class="size-4 animate-spin" />
        正在检查 catalog 引用……
      </div>

      <div
        v-else-if="refs.length > 0"
        class="space-y-3 rounded-lg bg-warning-50 p-3"
      >
        <div class="flex items-center gap-2 text-sm font-medium text-warning-700">
          <KunIcon name="lucide:triangle-alert" class="size-4" />
          该图片被 {{ refs.length }} 条 catalog 记录引用，删除后将留下空白图位
        </div>
        <ul class="space-y-1 text-sm text-warning-700">
          <li
            v-for="(item, index) in refs"
            :key="`${item.kind}-${item.entity_id}-${index}`"
            class="flex items-center gap-2"
          >
            <KunChip size="sm" color="warning" variant="flat">
              {{ kindLabel(item.kind) }}
            </KunChip>
            <span class="truncate">{{ item.label || '（无名）' }}</span>
            <span class="text-warning-600">#{{ item.entity_id }}</span>
          </li>
        </ul>
        <KunCheckBox
          v-model="detachRefs"
          color="warning"
          label="同时摘除 catalog 引用"
        />
      </div>

      <p v-else-if="refsFailed" class="text-sm text-danger">
        无法确认 catalog 引用（检查失败），请稍后重试再删除
      </p>

      <p v-else class="text-sm text-default-400">无 catalog 引用</p>

      <div class="flex justify-end gap-3">
        <KunButton
          color="default"
          variant="flat"
          :disabled="deleting"
          @click="open = false"
        >
          取消
        </KunButton>
        <KunButton
          :color="force ? 'danger' : 'warning'"
          :disabled="deleting || refsLoading"
          @click="confirmDelete"
        >
          <KunIcon
            v-if="deleting"
            name="lucide:loader-circle"
            class="mr-2 size-4 animate-spin"
          />
          {{ force ? '确认硬删除' : '确认软删除' }}
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>

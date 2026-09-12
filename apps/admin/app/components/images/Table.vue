<script setup lang="ts">
import { IMAGE_REVIEW_STATUS_MAP, IMAGE_VARIANT_DIMENSIONS } from '~/constants/admin'

interface Props {
  items: ImageAdminRow[]
}

const props = defineProps<Props>()
const _props = props

interface Emits {
  (e: 'review', hash: string, status: string, reason?: string): void
  (e: 'delete', hash: string, force: boolean): void
}

const emit = defineEmits<Emits>()

const statusColor = (s: string) =>
  IMAGE_REVIEW_STATUS_MAP[s]?.color ?? 'default'

const statusLabel = (s: string) => IMAGE_REVIEW_STATUS_MAP[s]?.label ?? s

const shortHash = (h: string) => `${h.slice(0, 8)}…${h.slice(-4)}`

const variantDims = (name: string) => {
  const d = IMAGE_VARIANT_DIMENSIONS[name]
  return d ? `${d.w}×${d.h}` : ''
}

const rejectOpen = ref(false)
const rejectHash = ref('')
const rejectReason = ref('')

const onReject = (hash: string) => {
  rejectHash.value = hash
  rejectReason.value = ''
  rejectOpen.value = true
}

const confirmReject = () => {
  emit('review', rejectHash.value, 'rejected', rejectReason.value || '')
  rejectOpen.value = false
}
</script>

<template>
  <KunLightboxGallery>
  <div class="overflow-x-auto rounded-xl bg-content1 shadow-sm">
    <table class="w-full min-w-[56rem] text-sm">
      <thead class="bg-content2 text-default-500">
        <tr>
          <th class="px-3 py-2 text-left font-medium">预览</th>
          <th class="px-3 py-2 text-left font-medium">Hash / 尺寸 / 变体</th>
          <th class="px-3 py-2 text-left font-medium">审核</th>
          <th class="px-3 py-2 text-left font-medium">上传</th>
          <th class="px-3 py-2 text-right font-medium">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="item in _props.items"
          :key="item.hash"
          class="border-t border-default-200 align-top"
        >
          <td class="px-3 py-2">
            <KunLightboxGalleryItem :src="item.url" :alt="item.hash">
              <img
                :src="item.variant_urls['100'] || item.variant_urls['mini'] || item.url"
                :alt="item.hash"
                class="size-16 rounded-md border border-default-200 object-cover"
                loading="lazy"
              />
            </KunLightboxGalleryItem>
          </td>
          <td class="px-3 py-2">
            <div class="flex items-center gap-1 font-mono">
              <span>{{ shortHash(item.hash) }}</span>
              <KunCopy :text="item.hash" size="xs" />
            </div>
            <div class="mt-0.5 text-xs text-default-400">
              {{ item.width }} × {{ item.height }} · {{ formatFileSize(item.size_bytes) }}
            </div>
            <div class="mt-0.5 text-xs text-default-400">
              {{ item.mime }}
            </div>
            <div class="mt-1.5 flex flex-wrap items-center gap-1">
              <KunTooltip :text="item.url" position="top">
                <a :href="item.url" target="_blank" rel="noopener">
                  <KunChip color="default" size="xs" variant="flat" class-name="gap-1">
                    <KunIcon name="lucide:image" class="size-3" />
                    原图 {{ item.width }}×{{ item.height }}
                  </KunChip>
                </a>
              </KunTooltip>
              <KunTooltip
                v-for="(url, name) in item.variant_urls"
                :key="name"
                :text="url"
                position="top"
              >
                <a :href="url" target="_blank" rel="noopener">
                  <KunChip color="success" size="xs" variant="flat" class-name="gap-1">
                    <KunIcon name="lucide:scaling" class="size-3" />
                    {{ name }}
                    <span v-if="variantDims(String(name))" class="text-success-700/70">
                      {{ variantDims(String(name)) }}
                    </span>
                  </KunChip>
                </a>
              </KunTooltip>
            </div>
          </td>
          <td class="px-3 py-2">
            <KunChip
              :color="statusColor(item.review_status)"
              variant="flat"
              size="xs"
            >
              {{ statusLabel(item.review_status) }}
            </KunChip>
            <div
              v-if="item.deleted_at"
              class="mt-0.5 text-xs text-danger-500"
            >
              已软删
            </div>
          </td>
          <td class="px-3 py-2 text-xs text-default-500">
            <div v-if="item.first_uploader_client">
              client: {{ item.first_uploader_client }}
            </div>
            <div v-if="item.first_uploader_sub" class="text-default-400">
              sub: {{ item.first_uploader_sub.slice(0, 12) }}…
            </div>
            <div class="mt-0.5 whitespace-nowrap text-default-400">
              {{ new Date(item.created_at).toLocaleString('zh-CN') }}
            </div>
          </td>
          <td class="px-3 py-2 text-right">
            <div class="inline-flex gap-1">
              <KunButton
                v-if="item.review_status !== 'approved'"
                color="success"
                variant="flat"
                size="sm"
                @click="emit('review', item.hash, 'approved')"
              >
                通过
              </KunButton>
              <KunButton
                v-if="item.review_status !== 'rejected'"
                color="danger"
                variant="flat"
                size="sm"
                @click="onReject(item.hash)"
              >
                拒绝
              </KunButton>
              <KunButton
                color="default"
                variant="flat"
                size="sm"
                @click="emit('delete', item.hash, false)"
              >
                软删
              </KunButton>
              <KunButton
                color="danger"
                variant="solid"
                size="sm"
                @click="emit('delete', item.hash, true)"
              >
                硬删
              </KunButton>
            </div>
          </td>
        </tr>
      </tbody>
    </table>

    <div v-if="!_props.items.length" class="px-3 py-8 text-center text-default-500">
      无数据
    </div>
  </div>
  </KunLightboxGallery>

  <KunModal v-model="rejectOpen" aria-label="拒绝图片">
    <div class="space-y-4">
      <h2 class="text-xl font-bold text-foreground">拒绝图片</h2>
      <p class="text-sm text-default-500">
        填写拒绝原因（可选），将记录到审核日志。
      </p>
      <KunTextarea
        v-model="rejectReason"
        label="拒绝原因"
        placeholder="可留空"
        :rows="3"
      />
      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="rejectOpen = false">
          取消
        </KunButton>
        <KunButton color="danger" @click="confirmReject">
          确认拒绝
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>

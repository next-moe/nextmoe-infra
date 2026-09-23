<script setup lang="ts">
const props = defineProps<{ item: ShopItem | null }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

const api = useApi()
const { upload, uploading, error: uploadError } = useImageUpload()

const name = ref('')
const description = ref('')
const staticAsset = ref<ShopAsset | null>(null)
const animatedAsset = ref<ShopAsset | null>(null)
const staticKey = ref('')
const animatedKey = ref('')
const staticInput = ref<HTMLInputElement | null>(null)
const animatedInput = ref<HTMLInputElement | null>(null)
const saving = ref(false)
const error = ref('')

const frozen = computed(
  () => props.item?.status === 'published' || props.item?.status === 'retired'
)

watch(open, (v) => {
  if (!v) return
  error.value = ''
  name.value = props.item?.name ?? ''
  description.value = props.item?.description ?? ''
  staticKey.value = props.item?.render.static ?? ''
  animatedKey.value = props.item?.render.animated ?? ''
  staticAsset.value = null
  animatedAsset.value = null
})

const staticUrl = computed(
  () => staticAsset.value?.url ?? props.item?.preview?.static_url ?? ''
)
const animatedUrl = computed(
  () => animatedAsset.value?.url ?? (animatedKey.value ? props.item?.preview?.animated_url : '') ?? ''
)

const pick = async (e: Event, kind: 'static' | 'animated') => {
  const file = (e.target as HTMLInputElement).files?.[0]
  ;(e.target as HTMLInputElement).value = ''
  if (!file) return
  const asset = await upload<ShopAsset>('/admin/shop/assets', file, file.name)
  if (!asset) {
    useKunMessage(uploadError.value || '上传失败', 'error')
    return
  }
  if (kind === 'static') {
    staticAsset.value = asset
    staticKey.value = asset.key
  } else {
    animatedAsset.value = asset
    animatedKey.value = asset.key
  }
}

const save = async () => {
  error.value = ''
  if (!name.value.trim() || !staticKey.value) {
    error.value = '名称和静态图是必填的'
    return
  }
  saving.value = true
  try {
    const body = {
      kind: 'avatar_frame',
      name: name.value.trim(),
      description: description.value.trim(),
      render: { static: staticKey.value, ...(animatedKey.value ? { animated: animatedKey.value } : {}) },
    }
    const res = props.item
      ? await api.put(`/admin/shop/items/${props.item.id}`, body)
      : await api.post('/admin/shop/items', body)
    if (res.code !== 0) {
      error.value = res.message || '保存失败'
      return
    }
    useKunMessage(props.item ? '已保存' : '已创建草稿，发布后才能上架', 'success')
    open.value = false
    emit('saved')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <KunModal v-model="open" :title="item ? '编辑头像框' : '新建头像框'" size="md">
    <div class="space-y-4">
      <div class="flex justify-center py-4">
        <KunAvatar
          :user="{
            id: 0,
            name: '预览',
            avatar: '',
            avatarDecoration: staticUrl ? { src: staticUrl, animatedSrc: animatedUrl || undefined } : null,
          }"
          size="original-sm"
          decoration="always"
          :is-navigation="false"
        />
      </div>

      <KunInput v-model="name" label="名称" placeholder="例如：樱花" required />
      <KunTextarea v-model="description" label="简介" placeholder="一句话介绍（可选）" :rows="2" />

      <div class="border-default-200 space-y-3 rounded-lg border p-4">
        <p class="text-default-500 text-xs">
          画布是头像的 1.2 倍的正方形，头像圆居中；四角和中心必须透明。静态图用 PNG（≤ 512 KB），动图用带透明通道的动态
          WebP（≤ 2 MB，和静态图同尺寸）。{{ frozen ? '已发布的物品不能更换素材。' : '' }}
        </p>
        <div class="flex flex-wrap gap-2">
          <input ref="staticInput" type="file" accept="image/png" class="hidden" aria-label="静态图文件" @change="pick($event, 'static')">
          <input ref="animatedInput" type="file" accept="image/webp" class="hidden" aria-label="动图文件" @change="pick($event, 'animated')">
          <KunButton size="sm" variant="flat" :disabled="frozen" :loading="uploading" @click="staticInput?.click()">
            {{ staticKey ? '更换静态图' : '上传静态图' }}
          </KunButton>
          <KunButton size="sm" variant="flat" :disabled="frozen" :loading="uploading" @click="animatedInput?.click()">
            {{ animatedKey ? '更换动图' : '上传动图（可选）' }}
          </KunButton>
          <KunButton
            v-if="animatedKey && !frozen"
            size="sm"
            variant="light"
            color="danger"
            @click="animatedKey = ''; animatedAsset = null"
          >
            去掉动图
          </KunButton>
        </div>
      </div>

      <div v-if="error" class="bg-danger-50 text-danger rounded-lg p-3 text-sm">{{ error }}</div>

      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">取消</KunButton>
        <KunButton color="primary" :loading="saving" @click="save">保存</KunButton>
      </div>
    </div>
  </KunModal>
</template>

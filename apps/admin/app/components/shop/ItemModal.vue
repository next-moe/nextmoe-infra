<script setup lang="ts">
import { SHOP_KIND_OPTIONS, SHOP_KINDS } from '~/constants/shop'

const props = defineProps<{ item: ShopItem | null }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ saved: [] }>()

const api = useApi()
const { upload, uploading, error: uploadError } = useImageUpload()
const shopSites = useShopSites()

const kind = ref<ShopKind>('avatar_frame')
const siteId = ref<number | ''>('')
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
const spec = computed(() => SHOP_KINDS[kind.value])

watch(open, (v) => {
  if (!v) return
  shopSites.load()
  error.value = ''
  kind.value = props.item?.kind ?? 'avatar_frame'
  siteId.value = props.item?.site_id ?? ''
  name.value = props.item?.name ?? ''
  description.value = props.item?.description ?? ''
  staticKey.value = props.item?.render.static ?? ''
  animatedKey.value = props.item?.render.animated ?? ''
  staticAsset.value = null
  animatedAsset.value = null
})

watch(kind, (next, prev) => {
  if (props.item || next === prev) return
  staticKey.value = ''
  animatedKey.value = ''
  staticAsset.value = null
  animatedAsset.value = null
})

const staticUrl = computed(
  () => staticAsset.value?.url ?? props.item?.preview?.static_url ?? ''
)
const animatedUrl = computed(
  () =>
    animatedAsset.value?.url ??
    (animatedKey.value ? props.item?.preview?.animated_url : '') ??
    ''
)

const pick = async (e: Event, which: 'static' | 'animated') => {
  const file = (e.target as HTMLInputElement).files?.[0]
  ;(e.target as HTMLInputElement).value = ''
  if (!file) return
  const asset = await upload<ShopAsset>('/admin/shop/assets', file, file.name, {
    kind: kind.value
  })
  if (!asset) {
    useKunMessage(uploadError.value || '上传失败', 'error')
    return
  }
  if (which === 'static') {
    staticAsset.value = asset
    staticKey.value = asset.key
  } else {
    animatedAsset.value = asset
    animatedKey.value = asset.key
  }
}

const clearAnimated = () => {
  animatedKey.value = ''
  animatedAsset.value = null
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
      kind: kind.value,
      site_id: siteId.value === '' ? null : siteId.value,
      name: name.value.trim(),
      description: description.value.trim(),
      render: {
        static: staticKey.value,
        ...(animatedKey.value ? { animated: animatedKey.value } : {})
      }
    }
    const res = props.item
      ? await api.put(`/admin/shop/items/${props.item.id}`, body)
      : await api.post('/admin/shop/items', body)
    if (res.code !== 0) {
      error.value = res.message || '保存失败'
      return
    }
    useKunMessage(
      props.item ? '已保存' : '已创建草稿，发布后才能上架',
      'success'
    )
    open.value = false
    emit('saved')
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    :title="item ? `编辑${spec.label}` : '新建物品'"
    size="md"
  >
    <div class="space-y-4">
      <div class="py-4">
        <ShopItemPreview
          :kind="kind"
          :static-url="staticUrl"
          :animated-url="animatedUrl"
          animate
        />
      </div>

      <div class="grid grid-cols-2 gap-3">
        <KunSelect
          v-model="kind"
          label="类型"
          :options="SHOP_KIND_OPTIONS"
          :disabled="!!item"
        />
        <KunSelect
          v-model="siteId"
          label="所属站点"
          :options="shopSites.options.value"
          :disabled="frozen"
          description="站点独有的物品只能在该站点的专区出售"
        />
      </div>
      <KunInput v-model="name" label="名称" placeholder="例如：樱花" required />
      <KunTextarea
        v-model="description"
        label="简介"
        placeholder="一句话介绍（可选）"
        :rows="2"
      />

      <div class="border-default-200 space-y-3 rounded-lg border p-4">
        <p class="text-default-500 text-xs">
          {{ spec.hint }}{{ frozen ? '已发布的物品不能更换素材。' : '' }}
        </p>
        <div class="flex flex-wrap gap-2">
          <input
            ref="staticInput"
            type="file"
            :accept="spec.staticAccept"
            class="hidden"
            aria-label="静态图文件"
            @change="pick($event, 'static')"
          />
          <input
            ref="animatedInput"
            type="file"
            accept="image/webp"
            class="hidden"
            aria-label="动图文件"
            @change="pick($event, 'animated')"
          />
          <KunButton
            size="sm"
            variant="flat"
            :disabled="frozen"
            :loading="uploading"
            @click="staticInput?.click()"
          >
            {{ staticKey ? '更换静态图' : '上传静态图' }}
          </KunButton>
          <KunButton
            size="sm"
            variant="flat"
            :disabled="frozen"
            :loading="uploading"
            @click="animatedInput?.click()"
          >
            {{ animatedKey ? '更换动图' : '上传动图（可选）' }}
          </KunButton>
          <KunButton
            v-if="animatedKey && !frozen"
            size="sm"
            variant="light"
            color="danger"
            @click="clearAnimated"
          >
            去掉动图
          </KunButton>
        </div>
      </div>

      <div v-if="error" class="bg-danger-50 text-danger rounded-lg p-3 text-sm">
        {{ error }}
      </div>

      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false"
          >取消</KunButton
        >
        <KunButton color="primary" :loading="saving" @click="save"
          >保存</KunButton
        >
      </div>
    </div>
  </KunModal>
</template>

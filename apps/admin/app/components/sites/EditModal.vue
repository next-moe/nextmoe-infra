<script setup lang="ts">
const props = defineProps<{ site: Site | null }>()
const emit = defineEmits<{ updated: [] }>()

const open = defineModel<boolean>('open', { required: true })
const api = useApi()

const name = ref('')
const description = ref('')
const error = ref('')
const isLoading = ref(false)

watch(open, (v) => {
  if (!v || !props.site) return
  name.value = props.site.name
  description.value = props.site.description
  error.value = ''
})

const handleSubmit = async () => {
  const site = props.site
  if (!site) return
  if (!name.value.trim()) {
    error.value = '请填写站点名称'
    return
  }
  error.value = ''
  isLoading.value = true
  try {
    const response = await api.put(`/sites/${site.id}`, {
      name: name.value,
      description: description.value,
    })
    if (response.code === 0) {
      emit('updated')
    } else {
      error.value = response.message || '更新失败'
    }
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <KunModal v-model="open" aria-label="编辑站点">
    <div class="w-96 space-y-4 p-6">
      <h2 class="text-xl font-bold text-foreground">编辑站点</h2>

      <KunInput
        v-model="name"
        label="站点名称"
        placeholder="站点名称"
        required
      />

      <div>
        <p class="mb-1 text-sm font-medium text-default-500">域名</p>
        <p class="rounded-lg bg-default-100 px-3 py-2 text-sm text-default-400">
          {{ site?.domain }}
        </p>
      </div>

      <KunTextarea
        v-model="description"
        label="描述"
        placeholder="站点描述（可选）"
        :rows="3"
      />

      <div v-if="error" class="rounded-lg bg-danger-50 p-3 text-sm text-danger">
        {{ error }}
      </div>

      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">
          取消
        </KunButton>
        <KunButton color="primary" :disabled="isLoading" @click="handleSubmit">
          <KunIcon v-if="isLoading" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
          保存
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>

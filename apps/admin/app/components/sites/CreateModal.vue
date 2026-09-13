<script setup lang="ts">
const show = defineModel<boolean>({ required: true })
const emit = defineEmits<{ created: [] }>()

const api = useApi()

const name = ref('')
const domain = ref('')
const description = ref('')
const error = ref('')
const isLoading = ref(false)

const handleSubmit = async () => {
  error.value = ''
  if (!name.value || !domain.value) {
    error.value = '名称和域名为必填项'
    return
  }

  isLoading.value = true
  try {
    const response = await api.post<Site>('/sites', {
      name: name.value,
      domain: domain.value,
      description: description.value,
    })
    if (response.code === 0) {
      name.value = ''
      domain.value = ''
      description.value = ''
      emit('created')
    } else {
      error.value = response.message || '创建失败'
    }
  } finally {
    isLoading.value = false
  }
}
</script>

<template>
  <KunModal v-model="show" aria-label="添加站点">
    <div class="w-96 space-y-4 p-6">
      <h2 class="text-xl font-bold text-foreground">添加站点</h2>

      <KunInput
        v-model="name"
        label="站点名称"
        placeholder="例如：KUN Galgame"
        required
      />

      <KunInput
        v-model="domain"
        label="域名"
        placeholder="例如：www.kungal.com"
        required
      />

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
        <KunButton color="default" variant="flat" @click="show = false">
          取消
        </KunButton>
        <KunButton color="primary" :disabled="isLoading" @click="handleSubmit">
          <KunIcon v-if="isLoading" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
          创建
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>

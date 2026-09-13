<script setup lang="ts">
const open = defineModel<boolean>('open', { required: true })
const props = defineProps<{ user: { uuid: string; name: string } | null }>()
const emit = defineEmits<{ success: [] }>()

const api = useApi()

const name = ref('')
const email = ref('')
const bio = ref('')
const initial = ref<{ name: string; email: string; bio: string } | null>(null)

// The API blanks the email for a caller without `oauth.users.pii_view`, and the
// same permission now gates writing it. An empty string here means "redacted
// from you", never "this user has no email" — reading it as the latter and
// making the field required locks every non-ren admin out of fixing a name.
const emailVisible = computed(() => !!initial.value?.email)

const loading = ref(false)
const loadFailed = ref(false)
const error = ref('')
const submitting = ref(false)

let loadToken = 0

const load = async (uuid: string) => {
  const token = ++loadToken
  loading.value = true
  loadFailed.value = false
  error.value = ''
  initial.value = null
  name.value = ''
  email.value = ''
  bio.value = ''

  const res = await api.get<Pick<User, 'name' | 'email' | 'bio'>>(
    `/admin/users/${uuid}`
  )
  if (token !== loadToken) return

  if (res.code === 0 && res.data) {
    name.value = res.data.name ?? ''
    email.value = res.data.email ?? ''
    bio.value = res.data.bio ?? ''
    initial.value = { name: name.value, email: email.value, bio: bio.value }
  } else {
    error.value = res.message || '加载用户信息失败'
    loadFailed.value = true
  }
  loading.value = false
}

watch([open, () => props.user?.uuid], () => {
  if (open.value && props.user) load(props.user.uuid)
})

const dirtyPatch = computed(() => {
  if (!initial.value) return null
  const patch: { name?: string; email?: string; bio?: string } = {}
  if (name.value.trim() !== initial.value.name) {
    patch.name = name.value.trim()
  }
  if (emailVisible.value && email.value.trim() !== initial.value.email) {
    patch.email = email.value.trim()
  }
  if (bio.value !== initial.value.bio) {
    patch.bio = bio.value
  }
  return Object.keys(patch).length ? patch : null
})

const handleSubmit = async () => {
  if (submitting.value || !props.user || loadFailed.value) return
  error.value = ''

  const patch = dirtyPatch.value
  if (!patch) {
    error.value = '没有任何改动'
    return
  }
  if (patch.name !== undefined && (patch.name.length < 2 || patch.name.length > 17)) {
    error.value = '用户名长度需为 2–17 个字符'
    return
  }
  if (patch.bio !== undefined && patch.bio.length > 107) {
    error.value = '个人简介不能超过 107 个字符'
    return
  }
  if (patch.email !== undefined && !patch.email.includes('@')) {
    error.value = '请填写有效的邮箱地址'
    return
  }

  submitting.value = true
  try {
    const res = await api.patch(`/admin/users/${props.user.uuid}`, patch)
    if (res.code === 0) {
      useKunMessage('资料已更新', 'success')
      emit('success')
      open.value = false
    } else {
      error.value = res.message || '更新失败'
    }
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <KunModal v-model="open" aria-label="编辑用户资料">
    <div class="space-y-4">
      <div>
        <h2 class="text-xl font-bold text-foreground">编辑用户资料</h2>
        <p class="mt-1 text-sm text-default-500">
          用户
          <span class="font-semibold text-foreground">{{ user?.name }}</span>
        </p>
      </div>

      <div v-if="loading" class="flex justify-center py-8">
        <KunIcon
          name="lucide:loader-circle"
          class="size-6 animate-spin text-primary"
        />
      </div>

      <form v-else class="space-y-4" @submit.prevent="handleSubmit">
        <KunInput
          v-model="name"
          label="用户名"
          placeholder="2–17 个字符，全站唯一"
          autocomplete="off"
        />

        <div v-if="emailVisible" class="space-y-1">
          <KunInput
            v-model="email"
            type="email"
            label="邮箱"
            autocomplete="off"
          />
          <p class="text-xs text-warning-500">
            管理员改邮箱不会发送验证码，保存后立即生效。
          </p>
        </div>
        <div v-else class="space-y-1">
          <p class="text-sm font-medium text-default-500">邮箱</p>
          <p class="rounded-lg bg-default-100 p-3 text-sm text-default-400">
            已隐藏 —— 查看和修改邮箱需要「查看用户 PII」权限。
          </p>
        </div>

        <KunTextarea
          v-model="bio"
          label="个人简介"
          placeholder="一句话介绍（≤107 字）"
          :rows="3"
        />

        <p v-if="error" class="rounded-lg bg-danger-50 p-3 text-sm text-danger">
          {{ error }}
        </p>

        <div class="flex justify-end gap-3">
          <KunButton
            color="default"
            variant="flat"
            type="button"
            :disabled="submitting"
            @click="open = false"
          >
            取消
          </KunButton>
          <KunButton
            color="primary"
            type="submit"
            :disabled="submitting || loadFailed"
          >
            <KunIcon
              v-if="submitting"
              name="lucide:loader-circle"
              class="mr-2 size-4 animate-spin"
            />
            {{ submitting ? '保存中...' : '保存' }}
          </KunButton>
        </div>
      </form>
    </div>
  </KunModal>
</template>

<script setup lang="ts">
import type { CanonTodo, CanonTodoList } from '#shared/types/canon'
import { CANON_TODO_KINDS } from '~/constants/canon'
import { problemStatus } from '~/utils/review/problem'

useHead({ title: '采集待办 · 未萌编辑台' })

const KIND_OPTIONS = [
  { value: '', label: '全部' },
  ...Object.entries(CANON_TODO_KINDS).map(([value, k]) => ({
    value,
    label: k.label
  }))
]

const STATE_OPTIONS = [
  { value: 'open', label: '待处理' },
  { value: 'done', label: '已接受' },
  { value: 'rejected', label: '已驳回' }
]

const api = useEditorApi()
const route = useRoute()
const router = useRouter()

const queryValue = (key: string, options: { value: string }[], fallback: string) =>
  computed({
    get: () =>
      options.some((o) => o.value === route.query[key])
        ? String(route.query[key])
        : fallback,
    set: (value: string) =>
      router.replace({
        query: { ...route.query, [key]: value === fallback ? undefined : value }
      })
  })

const kind = queryValue('kind', KIND_OPTIONS, '')
const state = queryValue('state', STATE_OPTIONS, 'open')

const items = ref<CanonTodo[]>([])
const nextCursor = ref<string | null>(null)
const loading = ref(true)
const forbidden = ref(false)

const fetchPage = async (cursor: string | null) => {
  loading.value = true
  try {
    const list = await api<CanonTodoList>('/v3/catalog/todos', {
      query: {
        limit: 30,
        state: state.value,
        ...(kind.value ? { kind: kind.value } : {}),
        ...(cursor ? { cursor } : {})
      }
    })
    forbidden.value = false
    items.value = cursor ? [...items.value, ...list.items] : list.items
    nextCursor.value = list.next_cursor ?? null
  } catch (error) {
    forbidden.value = problemStatus(error) === 403
    if (!cursor) {
      items.value = []
    }
    nextCursor.value = null
  } finally {
    loading.value = false
  }
}

const settled = (id: string) => {
  items.value = items.value.filter((t) => t.id !== id)
}

watch([kind, state], () => fetchPage(null))
onMounted(() => fetchPage(null))
</script>

<template>
  <div class="mx-auto max-w-6xl space-y-5">
    <KunHeader
      name="采集待办"
      description="采集程序提交的数据认不出是哪个已有条目，或撞上了人写过的字段时，会停在这里等人裁决。接受即以你的名义写入；驳回会告诉采集端不再提交同样的数据。"
    />

    <div class="flex flex-wrap items-center gap-3">
      <KunRadioGroup
        v-model="kind"
        :options="KIND_OPTIONS"
        variant="pill"
        orientation="horizontal"
        size="sm"
        aria-label="种类"
      />
      <KunRadioGroup
        v-model="state"
        :options="STATE_OPTIONS"
        variant="pill"
        orientation="horizontal"
        size="sm"
        aria-label="状态"
      />
    </div>

    <KunInfo
      v-if="forbidden"
      color="warning"
      variant="flat"
      title="需要采集待办的审核权限"
      description="身份待办需要 canon.todo.identity，取值待办需要 canon.todo.value；默认授予版主、管理员。"
    />
    <KunCard v-else bordered padding="none">
      <div v-if="loading && !items.length" class="p-4">
        <CommonSkeletonRows :rows="5" trailing />
      </div>
      <ul v-else-if="items.length" class="divide-default-200 divide-y">
        <ReviewIngestTodoRow
          v-for="todo in items"
          :key="todo.id"
          :todo="todo"
          @settled="settled"
        />
      </ul>
      <div v-else-if="!loading" class="p-4">
        <KunNull description="没有这个状态的待办" />
      </div>
      <div class="flex justify-center p-3">
        <KunLoading v-if="loading && items.length" spinner />
        <KunButton
          v-else-if="nextCursor"
          variant="bordered"
          @click="fetchPage(nextCursor)"
        >
          加载更多
        </KunButton>
      </div>
    </KunCard>
  </div>
</template>

<script setup lang="ts">
import type {
  CanonTodo,
  CanonTodoResult,
  CanonTodoSheet
} from '#shared/types/canon'
import { CANON_TODO_KINDS } from '~/constants/canon'
import { answersFrom, unanswered } from '~/utils/canon/todo'
import { formatRelative } from '~/utils/format'
import { problemMessage } from '~/utils/review/problem'

const props = defineProps<{ todo: CanonTodo }>()
const emit = defineEmits<{ settled: [id: string] }>()

const api = useEditorApi()
const open = ref(false)
const busy = ref(false)
const rejecting = ref(false)
const note = ref('')
const sheet = ref<CanonTodoSheet | null>(null)
const chosen = ref<Record<string, string>>({})
const omitted = ref<string[]>([])

const kind = computed(() => CANON_TODO_KINDS[props.todo.kind])
const decidable = computed(() => props.todo.state === 'open')
const pending = computed(() =>
  unanswered(sheet.value?.questions ?? [], chosen.value, omitted.value)
)

const toggle = async () => {
  open.value = !open.value
  if (!open.value || sheet.value) {
    return
  }
  try {
    sheet.value = await api<CanonTodoSheet>(`/v3/catalog/todos/${props.todo.id}`)
  } catch (error) {
    useKunMessage(problemMessage(error), 'error')
  }
}

const decide = async (action: 'accept' | 'reject') => {
  busy.value = true
  try {
    const body =
      action === 'accept'
        ? {
            answers: answersFrom(
              sheet.value?.questions ?? [],
              chosen.value,
              omitted.value
            ),
            omit: omitted.value
          }
        : { note: note.value.trim() }
    await api<CanonTodoResult>(
      `/v3/catalog/todos/${props.todo.id}/${action}`,
      { method: 'POST', body }
    )
    useKunMessage(action === 'accept' ? '已接受并写入' : '已驳回', 'success')
    rejecting.value = false
    emit('settled', props.todo.id)
  } catch (error) {
    useKunMessage(problemMessage(error), 'error')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <li class="px-4 py-3">
    <button
      type="button"
      class="flex w-full items-start gap-3 text-left"
      :aria-expanded="open"
      @click="toggle"
    >
      <KunChip size="sm" variant="flat" class-name="mt-0.5 shrink-0">
        {{ kind?.label ?? props.todo.kind }}
      </KunChip>
      <div class="min-w-0 flex-1 space-y-1">
        <p class="text-foreground truncate font-medium">
          {{ props.todo.subject_key }}
        </p>
        <p class="text-default-500 text-sm">{{ props.todo.detail }}</p>
        <p class="text-default-400 text-xs">
          #{{ props.todo.id }} · {{ props.todo.program }} ·
          {{ props.todo.source }} ·
          {{ formatRelative(props.todo.created_at) }}
          <template v-if="props.todo.resolved_by">
            · {{ props.todo.resolved_by }}
          </template>
        </p>
      </div>
      <KunIcon
        :name="open ? 'lucide:chevron-up' : 'lucide:chevron-down'"
        class="text-default-400 mt-1 size-4 shrink-0"
      />
    </button>

    <div v-if="open" class="mt-3 space-y-3 pl-1">
      <p v-if="kind" class="text-default-500 text-sm">{{ kind.description }}</p>
      <KunLoading v-if="!sheet" spinner />
      <ReviewIngestSheet
        v-else
        v-model:chosen="chosen"
        v-model:omitted="omitted"
        :sheet="sheet"
        :editable="decidable"
      />

      <div v-if="decidable && sheet" class="space-y-2">
        <KunTextarea
          v-if="rejecting"
          v-model="note"
          placeholder="驳回理由（采集端会看到）"
          aria-label="驳回理由"
          max-height="160px"
        />
        <div class="flex items-center justify-end gap-2">
          <p v-if="pending.length && !rejecting" class="text-default-500 text-sm">
            还有 {{ pending.length }} 个身份问题没选
          </p>
          <template v-if="rejecting">
            <KunButton variant="light" @click="rejecting = false">取消</KunButton>
            <KunButton
              color="danger"
              :loading="busy"
              :disabled="!note.trim()"
              @click="decide('reject')"
            >
              确认驳回
            </KunButton>
          </template>
          <template v-else>
            <KunButton variant="light" color="danger" @click="rejecting = true">
              驳回
            </KunButton>
            <KunButton
              color="primary"
              :loading="busy"
              :disabled="pending.length > 0"
              @click="decide('accept')"
            >
              接受
            </KunButton>
          </template>
        </div>
      </div>
    </div>
  </li>
</template>

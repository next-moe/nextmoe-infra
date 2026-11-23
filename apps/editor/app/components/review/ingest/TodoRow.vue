<script setup lang="ts">
import type { CanonTodo, CanonTodoResult } from '#shared/types/canon'
import { CANON_ENTITY_LABELS, CANON_TODO_KINDS } from '~/constants/canon'
import { citableRefs, suggestionLines, useIDsFrom } from '~/utils/canon/todo'
import { formatRelative } from '~/utils/format'
import { problemMessage } from '~/utils/review/problem'

const props = defineProps<{ todo: CanonTodo }>()
const emit = defineEmits<{ settled: [id: string] }>()

const api = useEditorApi()
const open = ref(false)
const busy = ref(false)
const rejecting = ref(false)
const note = ref('')
const cited = reactive<Record<string, string>>({})

const kind = computed(() => CANON_TODO_KINDS[props.todo.kind])
const lines = computed(() => suggestionLines(props.todo.suggestion))
const citable = computed(() =>
  props.todo.kind === 'identity' ? citableRefs(props.todo.suggestion) : []
)
const decidable = computed(() => props.todo.state === 'open')

const decide = async (action: 'accept' | 'reject') => {
  busy.value = true
  try {
    const body =
      action === 'accept'
        ? { use_ids: useIDsFrom(cited) }
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
      @click="open = !open"
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
          #{{ props.todo.id }} · {{ props.todo.source }} ·
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
      <div class="border-default-200 divide-default-200 divide-y rounded-lg border">
        <div v-for="line in lines" :key="line.ref" class="space-y-1 px-3 py-2">
          <p class="text-sm">
            <span class="text-foreground font-medium">{{
              CANON_ENTITY_LABELS[line.entity] ?? line.entity
            }}</span>
            <span class="text-default-400 ml-2 font-mono text-xs">{{
              line.ref
            }}</span>
          </p>
          <dl class="grid grid-cols-[8rem_1fr] gap-x-3 gap-y-0.5 text-sm">
            <template v-for="v in line.values" :key="v.key">
              <dt class="text-default-500 font-mono text-xs leading-5">
                {{ v.key }}
              </dt>
              <dd class="text-foreground break-all">
                {{ v.value }}
                <span v-if="v.tier" class="text-default-400 text-xs">
                  · {{ v.tier }}
                </span>
              </dd>
            </template>
          </dl>
        </div>
      </div>

      <div v-if="decidable && citable.length" class="space-y-2">
        <p class="text-default-500 text-sm">
          已有同一个条目时填它的编号，这条记录就引用它而不新建；人物别名要连同所属人物一起填。
        </p>
        <div class="grid gap-2 sm:grid-cols-2">
          <KunInput
            v-for="c in citable"
            :key="c.ref"
            v-model="cited[c.ref]"
            :label="`${CANON_ENTITY_LABELS[c.entity] ?? c.entity} ${c.ref}`"
            placeholder="已有条目编号（留空即新建）"
            inputmode="numeric"
          />
        </div>
      </div>

      <div v-if="decidable" class="space-y-2">
        <KunTextarea
          v-if="rejecting"
          v-model="note"
          placeholder="驳回理由（采集端会看到）"
          aria-label="驳回理由"
          max-height="160px"
        />
        <div class="flex justify-end gap-2">
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
            <KunButton color="primary" :loading="busy" @click="decide('accept')">
              接受
            </KunButton>
          </template>
        </div>
      </div>
    </div>
  </li>
</template>

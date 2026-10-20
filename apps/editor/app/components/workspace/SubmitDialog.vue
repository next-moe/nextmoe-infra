<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { ENTITY_META } from '~/constants/editEntities'
import type {
  WorkspaceEntity,
  WorkspaceSubmission
} from '~/composables/useWorkspaceSession'

const NOTE_MAX = 2000

const open = defineModel<boolean>({ required: true })

const session = useWorkspaceSession()

type RowState = 'idle' | 'sending' | 'done' | 'failed'

const note = ref('')
const checking = ref(false)
const submitting = ref(false)
const conflicts = ref<Record<string, string[]>>({})
const states = ref<Record<string, RowState>>({})
const errors = ref<Record<string, string>>({})
const results = ref<WorkspaceSubmission[]>([])

const entities = shallowRef<WorkspaceEntity[]>([])

watch(open, async (visible) => {
  if (!visible) {
    return
  }
  entities.value = [...session.dirty.value]
  states.value = {}
  errors.value = {}
  results.value = []
  checking.value = true
  const found = await Promise.all(
    entities.value.map(
      async (entity) =>
        [entity.key, await entity.workspace.editor.findConflicts()] as const
    )
  )
  conflicts.value = Object.fromEntries(found.filter(([, keys]) => keys.length))
  checking.value = false
})

const pendingEntities = computed(() =>
  entities.value.filter((entity) => states.value[entity.key] !== 'done')
)
const finished = computed(
  () =>
    entities.value.length > 0 &&
    entities.value.every((entity) => states.value[entity.key] === 'done')
)

const submit = async () => {
  submitting.value = true
  for (const entity of pendingEntities.value) {
    states.value = { ...states.value, [entity.key]: 'sending' }
    const result = await session.submit(entity, note.value.trim())
    if (result.ok) {
      states.value = { ...states.value, [entity.key]: 'done' }
      const submission = session.submissions.value.find(
        (entry) => entry.key === entity.key
      )
      if (submission) {
        results.value = [...results.value, submission]
      }
    } else {
      states.value = { ...states.value, [entity.key]: 'failed' }
      errors.value = { ...errors.value, [entity.key]: result.message }
    }
  }
  submitting.value = false
  if (finished.value) {
    note.value = ''
    const merged = results.value.filter((r) => r.proposal.state === 'merged')
    useKunMessage(
      merged.length === results.value.length
        ? '修改已生效'
        : `已提交 ${results.value.length} 个条目的修改`,
      'success'
    )
  }
}

const stateIcon: Record<RowState, string> = {
  idle: '',
  sending: 'lucide:loader-circle',
  done: 'lucide:circle-check',
  failed: 'lucide:circle-x'
}
</script>

<template>
  <KunModal
    v-model="open"
    :title="finished ? '提交完成' : '提交修改'"
    :description="
      finished
        ? '修改已经交给资料库处理。'
        : '每个条目会生成一条独立的修改记录，共用下面的修改说明。'
    "
    size="lg"
    :is-dismissable="!submitting"
  >
    <div class="space-y-4">
      <ul
        class="border-default-200 divide-default-100 divide-y rounded-lg border"
      >
        <li
          v-for="entity in entities"
          :key="entity.key"
          class="flex flex-wrap items-center gap-2 px-3 py-2.5 text-sm"
        >
          <KunIcon
            :name="ENTITY_META[entity.family].icon"
            class="text-default-400 size-4"
          />
          <span class="text-default-500">
            {{ ENTITY_META[entity.family].label }}
          </span>
          <span class="min-w-0 flex-1 truncate font-medium">
            {{ session.nameOf(entity) }}
          </span>
          <template v-if="states[entity.key] === 'done'">
            <NuxtLink
              v-for="result in results.filter((r) => r.key === entity.key)"
              :key="result.proposal.id"
              :to="`/me/proposals/${result.proposal.id}`"
              target="_blank"
              class="text-primary-600 text-xs hover:underline"
            >
              {{ result.proposal.state === 'merged' ? '已生效' : '待审核' }} ·
              提案 #{{ result.proposal.id }}
            </NuxtLink>
          </template>
          <template v-else>
            <span class="text-default-400 text-xs tabular-nums">
              {{ changedParentKeys(entity.workspace).length }} 项
            </span>
            <KunChip
              size="xs"
              variant="flat"
              :color="
                entity.workspace.editor.willAutomerge.value
                  ? 'success'
                  : 'warning'
              "
            >
              {{
                entity.workspace.editor.willAutomerge.value
                  ? '直接生效'
                  : '需审核'
              }}
            </KunChip>
          </template>
          <KunIcon
            v-if="states[entity.key] && states[entity.key] !== 'idle'"
            :name="stateIcon[states[entity.key]!]"
            :class="
              cn(
                'size-4',
                states[entity.key] === 'sending' &&
                  'text-primary-500 animate-spin',
                states[entity.key] === 'done' && 'text-success-500',
                states[entity.key] === 'failed' && 'text-danger-500'
              )
            "
          />
          <p
            v-if="
              conflicts[entity.key]?.length && states[entity.key] !== 'done'
            "
            class="text-danger-600 w-full text-xs"
          >
            你编辑期间这些字段已被他人修改，提交会覆盖它们：{{
              conflicts[entity.key]!.map((key) => fieldLabel(key)).join('、')
            }}
          </p>
          <p v-if="errors[entity.key]" class="text-danger-600 w-full text-xs">
            {{ errors[entity.key] }}
          </p>
        </li>
      </ul>

      <template v-if="!finished">
        <KunLoading
          v-if="checking"
          spinner
          description="正在检查条目是否有新的改动…"
        />
        <KunTextarea
          v-model="note"
          label="修改说明"
          placeholder="简单说明修改原因和资料出处，方便审核者判断"
          :rows="3"
          auto-grow
          max-height="192px"
          :description="`${note.length} / ${NOTE_MAX}`"
          :error="note.length > NOTE_MAX ? `最多 ${NOTE_MAX} 字` : ''"
        />
      </template>

      <div class="flex justify-end gap-2">
        <KunButton
          variant="light"
          color="default"
          :disabled="submitting"
          @click="open = false"
        >
          {{ finished ? '继续编辑' : '返回修改' }}
        </KunButton>
        <KunButton
          v-if="!finished"
          color="primary"
          :loading="submitting"
          :disabled="
            checking || note.length > NOTE_MAX || !pendingEntities.length
          "
          @click="submit"
        >
          {{
            session.willAutomerge.value
              ? `提交并生效（${pendingEntities.length}）`
              : `提交（${pendingEntities.length}）`
          }}
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>

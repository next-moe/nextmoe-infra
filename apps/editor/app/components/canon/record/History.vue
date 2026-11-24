<script setup lang="ts">
import type { CanonPage, CanonRevision } from '#shared/types/canon'
import { CANON_ACTIONS, CANON_ENTITY_LABELS } from '~/constants/canon'
import { canonMessage } from '~/utils/canon/record'
import { formatRelative } from '~/utils/format'

const props = defineProps<{ entity: string; id: string }>()
const emit = defineEmits<{ undone: [] }>()

const api = useEditorApi()
const items = ref<CanonRevision[]>([])
const next = ref<string | null>(null)
const busy = ref('')

const load = async (cursor: string | null) => {
  try {
    const page = await api<CanonPage<CanonRevision>>('/v3/catalog/revisions', {
      query: {
        entity: props.entity,
        id: props.id,
        root: true,
        limit: 30,
        ...(cursor ? { cursor } : {})
      }
    })
    items.value = cursor ? [...items.value, ...page.items] : page.items
    next.value = page.next_cursor ?? null
  } catch (error) {
    useKunMessage(canonMessage(error), 'error')
  }
}

const undo = async (revision: CanonRevision) => {
  const ok = await useKunAlert({
    title: '撤销这一次提交？',
    message: '会撤回同一次提交里的全部改动；之后又被改过、被引用或被用户收藏的不能撤。'
  })
  if (!ok) {
    return
  }
  busy.value = revision.batch
  try {
    await api(`/v3/catalog/commits/${revision.batch}/undo`, {
      method: 'POST',
      body: { note: '编辑台撤销' }
    })
    useKunMessage('已撤销', 'success')
    await load(null)
    emit('undone')
  } catch (error) {
    useKunMessage(canonMessage(error), 'error')
  } finally {
    busy.value = ''
  }
}

const reload = () => load(null)
defineExpose({ reload })
onMounted(() => load(null))
</script>

<template>
  <KunCard bordered padding="none">
    <ul class="divide-default-200 divide-y">
      <li v-for="r in items" :key="r.id" class="flex items-start gap-3 px-4 py-2">
        <div class="min-w-0 flex-1">
          <p class="text-foreground text-sm">
            {{ CANON_ACTIONS[r.action] ?? r.action }}
            {{ CANON_ENTITY_LABELS[r.entity] ?? r.entity }} #{{ r.entity_id }}
            <span class="text-default-500">
              {{ Object.keys(r.changes).join('、') }}
            </span>
          </p>
          <p class="text-default-400 text-xs">
            {{ r.actor_key }}
            <template v-if="r.reviewed_by"> · 审核 {{ r.reviewed_by }}</template>
            · {{ formatRelative(r.at) }}
            <template v-if="r.note"> · {{ r.note }}</template>
          </p>
        </div>
        <KunChip v-if="r.undone" size="sm" variant="flat">已撤销</KunChip>
        <KunButton
          v-else-if="r.batch && ['create', 'update', 'remove'].includes(r.action)"
          size="sm"
          variant="light"
          :loading="busy === r.batch"
          @click="undo(r)"
        >
          撤销
        </KunButton>
      </li>
    </ul>
    <div v-if="next" class="flex justify-center p-3">
      <KunButton variant="bordered" size="sm" @click="load(next)">加载更多</KunButton>
    </div>
  </KunCard>
</template>

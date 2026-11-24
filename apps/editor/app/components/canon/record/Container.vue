<script setup lang="ts">
import type { CanonEditChange, CanonRecord, CanonSchemas } from '#shared/types/canon'
import { CANON_ENTITY_LABELS } from '~/constants/canon'
import { canonCandidates, canonMessage } from '~/utils/canon/record'
import { problemStatus } from '~/utils/review/problem'

const REMOVABLE = ['work_title', 'org_name', 'credit', 'release_content']

const api = useEditorApi()
const route = useRoute()
const entity = computed(() => String(route.params.entity))
const id = computed(() => String(route.params.id))

const schemas = ref<CanonSchemas | null>(null)
const record = ref<CanonRecord | null>(null)
const missing = ref(false)
const busy = ref('')
const history = ref<{ reload: () => void } | null>(null)

useHead({ title: () => `${CANON_ENTITY_LABELS[entity.value]} #${id.value} · 未萌编辑台` })

const load = async () => {
  try {
    schemas.value ??= await api<CanonSchemas>('/v3/catalog/schemas')
    record.value = await api<CanonRecord>(`/v3/catalog/records/${entity.value}/${id.value}`)
    missing.value = false
  } catch (error) {
    missing.value = [404, 410].includes(problemStatus(error))
    if (!missing.value) {
      useKunMessage(canonMessage(error), 'error')
    }
  }
}

const groups = computed(() => {
  const schema = schemas.value?.entities.find((e) => e.name === entity.value)
  return Object.entries(record.value?.children ?? {})
    .filter(([, rows]) => rows.length)
    .map(([child, rows]) => ({
      child,
      rows,
      parentField: schema?.children[child] ?? ''
    }))
})

const save = async (change: CanonEditChange) => {
  busy.value = `${change.entity}:${change.id}`
  try {
    await api('/v3/catalog/commits', {
      method: 'POST',
      body: { note: '编辑台修改', changes: [change] }
    })
    useKunMessage('已保存', 'success')
    await load()
    history.value?.reload()
  } catch (error) {
    const candidates = canonCandidates(error)
    useKunMessage(
      candidates.length
        ? `已有同名条目：${candidates.map((c) => `${c.token} ${c.label}`).join('、')}`
        : canonMessage(error),
      'error'
    )
  } finally {
    busy.value = ''
  }
}

watch([entity, id], load)
onMounted(load)
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-5">
    <KunHeader
      :name="record?.called || `${CANON_ENTITY_LABELS[entity] ?? entity} #${id}`"
      description="每个字段下面写着是谁写的、凭哪一句原文。改过的字段会锁定，采集程序只能提议。"
    />
    <KunNull v-if="missing" description="没有这个条目，或者它已经被合并、移除、下架" />
    <CommonSkeletonRows v-else-if="!record || !schemas" :rows="6" />
    <template v-else>
      <KunCard bordered padding="none">
        <CanonRecordRow
          :record="record"
          :schemas="schemas"
          :busy="busy === `${record.entity}:${record.id}`"
          @save="save"
        />
      </KunCard>
      <section v-for="g in groups" :key="g.child" class="space-y-2">
        <h2 class="text-foreground font-medium">
          {{ CANON_ENTITY_LABELS[g.child] ?? g.child }}（{{ g.rows.length }}）
        </h2>
        <KunCard bordered padding="none">
          <div class="divide-default-200 divide-y">
            <CanonRecordRow
              v-for="r in g.rows"
              :key="r.id"
              :record="r"
              :schemas="schemas"
              :parent-field="g.parentField"
              :removable="REMOVABLE.includes(g.child)"
              :busy="busy === `${r.entity}:${r.id}`"
              @save="save"
            />
          </div>
        </KunCard>
      </section>
      <section class="space-y-2">
        <h2 class="text-foreground font-medium">修订记录</h2>
        <CanonRecordHistory :id="id" ref="history" :entity="entity" @undone="load" />
      </section>
    </template>
  </div>
</template>

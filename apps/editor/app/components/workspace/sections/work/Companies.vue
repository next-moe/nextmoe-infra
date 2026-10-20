<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { WorkspaceSectionDef } from '~/constants/workspace'
import { vocabLabel } from '~/constants/vocabLabels'
import type { RelatedRow, RelatedState } from '~/utils/relatedRows'

const props = defineProps<{ section: WorkspaceSectionDef }>()

const FIELD = 'catalog.work.labels'

const nav = useWorkspaceNav()
const { entityId, refs } = useEditorContext()
const related = useWorkRelated(entityId)
const labels = useRelatedField(FIELD)

const query = ref('')
const adding = ref(false)

const merged = computed(() =>
  mergeRelatedRows({
    fieldKey: FIELD,
    human: labels.list.value,
    base: labels.baseList.value,
    context: labels.contextRows.value,
    suppressed: labels.suppressed.value
  })
)

const roleOptions = computed(() => labels.memberOptions('kind'))
const roleText = (wire: unknown) => {
  const token = labels.tokenOf('kind', wire)
  return token ? vocabLabel('attribution_role', token) : '未知身份'
}

const companies = computed(() => {
  const byCompany = new Map<string, RelatedRow[]>()
  for (const row of merged.value) {
    const id = String(row.value.label_id ?? '')
    byCompany.set(id, [...(byCompany.get(id) ?? []), row])
  }
  return [...byCompany.entries()].map(([id, rows]) => {
    const company = related.display.value.companies[id]
    const live = rows.filter((row) => row.state !== 'hidden')
    const state: RelatedState = rows.some((row) => row.state === 'added')
      ? 'added'
      : !live.length
        ? 'hidden'
        : rows.some((row) => row.state === 'hidden' || row.state === 'changed')
          ? 'changed'
          : null
    return {
      id,
      rows,
      state,
      known: company?.name || refs.nameOf('company', id),
      name: company?.name || refs.nameOf('company', id) || `厂商 #${id}`,
      logo: company?.logo ?? null,
      roles: live.map((row) => roleText(row.value.kind)),
      sources: [
        ...new Set(
          live.map((row) => (row.origin === 'human' ? '人工' : row.source))
        )
      ]
    }
  })
})

type Company = (typeof companies.value)[number]

const filtered = computed(() =>
  companies.value.filter((company) =>
    matchesFilter(query.value, company.name, company.id, ...company.roles)
  )
)
const visible = computed(() =>
  filtered.value.filter((company) => company.state !== 'hidden')
)
const hidden = computed(() =>
  filtered.value.filter((company) => company.state === 'hidden')
)

const selected = computed(
  () => companies.value.find((company) => company.id === nav.item.value) ?? null
)
const select = (company: Company | null) => {
  nav.item.value = company?.id ?? null
}

const takenRoles = (companyId: string) =>
  new Set(
    merged.value
      .filter((row) => String(row.value.label_id ?? '') === companyId)
      .map((row) => String(row.value.kind))
  )

const addRole = (companyId: string, kind: unknown) => {
  if (takenRoles(companyId).has(String(kind))) {
    return
  }
  labels.append({ label_id: Number(companyId), kind: Number(kind) })
}

const removeRow = (row: RelatedRow) => {
  if (row.origin === 'human') {
    labels.removeAt(row.index)
  } else if (row.identity) {
    labels.toggleSuppressed(row.identity)
  }
}

const freeRoles = computed(() =>
  selected.value
    ? roleOptions.value.filter(
        (option) => !takenRoles(selected.value!.id).has(String(option.value))
      )
    : []
)

const draft = reactive<{
  company: string | null
  kind: number | string | null
}>({ company: null, kind: null })
watch(
  roleOptions,
  (options) => {
    if (draft.kind === null && options.length) {
      draft.kind =
        options.find((option) => option.token === 'developer')?.value ??
        options[0]!.value
    }
  },
  { immediate: true }
)

const draftTaken = computed(() =>
  draft.company && draft.kind !== null
    ? takenRoles(draft.company).has(String(draft.kind))
    : false
)

const addCompany = () => {
  if (!draft.company || draft.kind === null || draftTaken.value) {
    return
  }
  addRole(draft.company, draft.kind)
  related.ensureCompanies([draft.company])
  nav.item.value = draft.company
  draft.company = null
  adding.value = false
}

watch(
  () => companies.value.map((company) => company.id).join(','),
  (joined) => {
    const ids = joined ? joined.split(',') : []
    refs.resolve('company', ids)
    if (related.display.value.loaded) {
      related.ensureCompanies(ids)
    }
  },
  { immediate: true }
)
watch(
  () => related.display.value.loaded,
  (loaded) => {
    if (loaded) {
      related.ensureCompanies(companies.value.map((company) => company.id))
    }
  }
)

onMounted(() => related.load())
</script>

<template>
  <WorkspaceListDetail
    :has-selection="Boolean(selected)"
    list-label="厂商列表"
    @back="select(null)"
  >
    <template #header>
      <WorkspaceRelatedListHeader
        v-model:query="query"
        :title="props.section.label"
        :total="visible.length"
        placeholder="按厂商名或身份筛选"
        add-label="添加厂商"
        :can-add="!labels.disabled.value"
        :adding="adding"
        @add="adding = !adding"
      />
    </template>

    <template #list>
      <WorkspaceRelatedAddPanel
        v-if="adding"
        title="添加参与这部作品的厂商"
        hint="同一个厂商可以有多个身份，例如既是开发商也是发行商。"
      >
        <EditRefPicker
          v-model="draft.company"
          family="company"
          aria-label="搜索厂商"
          placeholder="搜索厂商名或输入 ID"
        />
        <WorkspaceRelatedChoice
          v-model="draft.kind"
          :options="roleOptions"
          label="身份"
        />
        <p v-if="draftTaken" class="text-danger-600 text-xs">
          这个厂商已经有这个身份了。
        </p>
        <div class="flex items-center gap-2">
          <KunButton
            size="sm"
            color="primary"
            :disabled="!draft.company || draftTaken"
            @click="addCompany"
          >
            添加
          </KunButton>
          <KunButton
            href="/new/company"
            target="_blank"
            size="sm"
            variant="light"
          >
            <KunIcon name="lucide:external-link" class="size-3.5" />
            新建厂商
          </KunButton>
        </div>
      </WorkspaceRelatedAddPanel>

      <WorkspaceSkeletonList
        v-if="!related.display.value.loaded && !companies.length"
        :rows="4"
      />
      <WorkspaceEmpty
        v-else-if="!companies.length"
        icon="lucide:building-2"
        title="还没有厂商"
        description="用右上角的「添加厂商」记录开发商、发行商或社团。"
      />
      <WorkspaceEmpty
        v-else-if="!filtered.length"
        icon="lucide:search-x"
        title="没有匹配的厂商"
      />

      <WorkspaceRelatedRow
        v-for="company in visible"
        :key="company.id"
        :title="company.name"
        :selected="selected?.id === company.id"
        :state="company.state"
        :picture="company.logo"
        icon="lucide:building-2"
        @select="select(company)"
      >
        <template #meta>
          <span
            v-for="role in company.roles"
            :key="role"
            class="bg-default-100 text-default-600 rounded px-1.5 leading-5"
          >
            {{ role }}
          </span>
        </template>
        <template #trailing>
          <WorkspaceSourceBadge
            v-for="source in company.sources"
            :key="source"
            :source="source"
            :human="source === '人工'"
          />
        </template>
      </WorkspaceRelatedRow>

      <WorkspaceRelatedHiddenGroup :count="hidden.length" label="已隐藏的厂商">
        <WorkspaceRelatedRow
          v-for="company in hidden"
          :key="company.id"
          :title="company.name"
          :selected="selected?.id === company.id"
          :state="company.state"
          :picture="company.logo"
          icon="lucide:building-2"
          @select="select(company)"
        />
      </WorkspaceRelatedHiddenGroup>
    </template>

    <template #detail>
      <WorkspaceEntityPanel
        v-if="selected"
        :key="selected.id"
        family="company"
        :entity-id="selected.id"
        :label="selected.known"
        :thumbnail="selected.logo"
        :subtitle="`厂商 · #${selected.id}`"
      >
        <template #context>
          <WorkspaceRelatedContextCard
            title="在本作中的身份"
            :state="selected.state"
          >
            <ul
              class="divide-default-100 border-default-200 divide-y rounded-lg border"
            >
              <li
                v-for="row in selected.rows"
                :key="row.key"
                class="flex items-center gap-2 px-3 py-2 text-sm"
              >
                <span
                  :class="
                    cn(
                      'min-w-0 flex-1 font-medium',
                      row.state === 'hidden' && 'text-default-400 line-through'
                    )
                  "
                >
                  {{ roleText(row.value.kind) }}
                </span>
                <KunChip
                  v-if="row.state === 'added'"
                  size="xs"
                  variant="flat"
                  color="success"
                >
                  新增
                </KunChip>
                <WorkspaceSourceBadge
                  :source="row.source"
                  :human="row.origin === 'human'"
                />
                <KunButton
                  size="xs"
                  variant="light"
                  :color="row.state === 'hidden' ? 'primary' : 'danger'"
                  :disabled="labels.disabled.value"
                  @click="removeRow(row)"
                >
                  {{
                    row.origin === 'human'
                      ? '删除'
                      : row.state === 'hidden'
                        ? '恢复'
                        : '隐藏'
                  }}
                </KunButton>
              </li>
            </ul>
            <div
              v-if="freeRoles.length && !labels.disabled.value"
              class="flex flex-wrap items-center gap-2"
            >
              <span class="text-default-500 text-xs">再添加一个身份</span>
              <KunButton
                v-for="option in freeRoles"
                :key="option.value"
                size="xs"
                variant="bordered"
                @click="addRole(selected.id, option.value)"
              >
                <KunIcon name="lucide:plus" class="size-3" />
                {{ option.label }}
              </KunButton>
            </div>
          </WorkspaceRelatedContextCard>
        </template>
      </WorkspaceEntityPanel>
    </template>
  </WorkspaceListDetail>
</template>

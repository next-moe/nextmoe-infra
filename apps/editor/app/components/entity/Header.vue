<script setup lang="ts">
import type { CatalogEntitySummary } from '#shared/types/catalog'
import { MERGEABLE_FAMILIES } from '~/constants/creation'
import { ENTITY_META, type EditFamily } from '~/constants/editEntities'
import { newIdempotencyKey, problemMessage } from '~/utils/review/problem'

const props = defineProps<{
  family: EditFamily
  entityId: string
  summary: CatalogEntitySummary | null
  mode: 'view' | 'edit'
  loading?: boolean
}>()

const meta = computed(() => ENTITY_META[props.family])
const name = computed(
  () =>
    props.summary?.display_name ||
    props.summary?.title ||
    `${meta.value.label} #${props.entityId}`
)
const mergeable = computed(
  () => props.mode === 'view' && MERGEABLE_FAMILIES.includes(props.family)
)
const mergeOpen = ref(false)

const unattributed = computed(
  () =>
    props.mode === 'view' &&
    props.family === 'credit_name' &&
    props.summary !== null &&
    (props.summary.person_id === null || props.summary.person_id === undefined)
)
const creatingPerson = ref(false)
const api = useEditorApi()

const createPerson = async () => {
  if (creatingPerson.value) {
    return
  }
  creatingPerson.value = true
  try {
    const proposal = await api<{
      id: string
      state: string
      entity_id: string
    }>('/v2/me/creations', {
      method: 'POST',
      headers: { 'Idempotency-Key': newIdempotencyKey() },
      body: {
        object: 'person',
        params: { credit_name_id: Number(props.entityId) }
      }
    })
    if (proposal.state === 'merged' && proposal.entity_id !== '0') {
      useKunMessage('人物已创建', 'success')
      await navigateTo(`/persons/${proposal.entity_id}/edit`)
      return
    }
    useKunMessage('新建人物的提案已提交，审核通过后生效', 'success')
    await navigateTo(`/me/proposals/${proposal.id}`)
  } catch (error) {
    useKunMessage(problemMessage(error, '新建人物失败'), 'error')
  } finally {
    creatingPerson.value = false
  }
}

const picture = computed(
  () =>
    props.summary?.image ?? props.summary?.photo ?? props.summary?.logo ?? null
)
</script>

<template>
  <div class="space-y-3">
    <NuxtLink
      :to="meta.path"
      class="text-default-500 hover:text-foreground inline-flex items-center gap-1 text-sm"
    >
      <KunIcon name="lucide:chevron-left" class="size-4" />
      {{ meta.label }}
    </NuxtLink>
    <header class="flex items-center gap-4">
      <div
        class="bg-default-100 flex size-16 shrink-0 items-center justify-center overflow-hidden rounded-lg sm:size-20"
      >
        <KunImage
          v-if="picture?.url"
          :src="picture.url"
          :thumbhash="picture.thumbhash ?? undefined"
          :alt="name"
          aspect-ratio="1 / 1"
          object-fit="cover"
          loading="eager"
        />
        <KunIcon v-else :name="meta.icon" class="text-default-300 size-7" />
      </div>
      <div class="min-w-0 flex-1">
        <p class="text-default-500 flex items-center gap-1 text-xs">
          <KunIcon
            :name="props.mode === 'edit' ? 'lucide:pencil' : meta.icon"
            class="size-3.5"
          />
          {{ props.mode === 'edit' ? `编辑${meta.label}` : meta.label }}
        </p>
        <KunSkeleton
          v-if="props.loading && !props.summary"
          variant="text"
          class-name="text-xl sm:text-2xl"
          width="14rem"
        />
        <h1 v-else class="truncate text-xl font-bold sm:text-2xl">
          {{ name }}
        </h1>
        <p
          class="text-default-500 mt-0.5 flex flex-wrap items-center gap-2 text-sm"
        >
          <span v-if="props.summary?.latin" class="truncate">
            {{ props.summary.latin }}
          </span>
          <span class="inline-flex items-center gap-1">
            条目 ID
            <KunCopy :text="props.entityId" />
          </span>
        </p>
      </div>
      <div class="flex shrink-0 flex-wrap justify-end gap-2">
        <KunButton
          v-if="unattributed"
          variant="light"
          size="sm"
          :loading="creatingPerson"
          @click="createPerson"
        >
          <KunIcon name="lucide:user-plus" class="size-4" />
          以此新建人物
        </KunButton>
        <KunButton
          v-if="mergeable"
          variant="light"
          size="sm"
          @click="mergeOpen = true"
        >
          <KunIcon name="lucide:git-merge" class="size-4" />
          合并
        </KunButton>
        <KunButton
          :href="`/history/${props.family}/${props.entityId}`"
          variant="light"
          size="sm"
        >
          <KunIcon name="lucide:history" class="size-4" />
          修订历史
        </KunButton>
        <KunButton
          v-if="props.mode === 'view'"
          :href="`${meta.path}/${props.entityId}/edit`"
          target="_blank"
          color="primary"
          size="sm"
        >
          <KunIcon name="lucide:pencil" class="size-4" />
          编辑
        </KunButton>
        <KunButton
          v-else
          :href="`${meta.path}/${props.entityId}`"
          variant="bordered"
          size="sm"
        >
          <KunIcon name="lucide:eye" class="size-4" />
          查看资料
        </KunButton>
      </div>
    </header>
    <MergeRequestModal
      v-if="mergeable"
      v-model="mergeOpen"
      :family="props.family"
      :entity-id="props.entityId"
      :entity-name="name"
    />
  </div>
</template>

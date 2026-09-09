<script setup lang="ts">
import { CATALOG_ENTITY_TYPES } from '~/constants/catalog'
import type {
  CatalogCandidateItem,
  CatalogEntitySummary,
  CatalogMergeDirection
} from '~~/shared/types/catalog'

const props = defineProps<{
  item: CatalogCandidateItem | null
  direction: CatalogMergeDirection
  busy: boolean
}>()

const open = defineModel<boolean>({ required: true })

const emit = defineEmits<{ confirm: [note: string] }>()

const note = ref('')
watch(open, (value) => {
  if (value) note.value = ''
})

const source = computed<CatalogEntitySummary | null>(() =>
  props.item ? (props.direction === 'ab' ? props.item.a : props.item.b) : null
)
const target = computed<CatalogEntitySummary | null>(() =>
  props.item ? (props.direction === 'ab' ? props.item.b : props.item.a) : null
)

const entityNoun = computed(() =>
  props.item ? (CATALOG_ENTITY_TYPES[props.item.entity_type] ?? '实体') : '实体'
)

const nameOf = (s: CatalogEntitySummary | null) =>
  s ? s.display_name || `#${s.id}` : ''

const releasesClaim = computed(
  () => !!props.item?.a.site && !!props.item?.b.site
)
</script>

<template>
  <KunModal v-model="open" title="确认合并" size="md">
    <div class="space-y-4">
      <div class="border-default-200 space-y-2 rounded-lg border p-3">
        <p class="text-default-500 text-sm">这次合并会：</p>
        <div class="flex flex-wrap items-center gap-2">
          <KunChip color="danger" variant="flat" size="sm">
            并入 {{ nameOf(source) }} · #{{ source?.id }}
          </KunChip>
          <KunIcon name="lucide:arrow-right" class="text-default-400 size-4" />
          <KunChip color="success" variant="solid" size="sm">
            保留 {{ nameOf(target) }} · #{{ target?.id }}
          </KunChip>
        </div>
        <p class="text-default-400 text-xs">
          被并入的{{
            entityNoun
          }}会退出身份索引，其外部锚点与关系将改挂到保留侧。
        </p>
      </div>

      <div
        class="border-info-200 bg-info-50 flex items-start gap-2 rounded-lg border px-3 py-2"
      >
        <KunIcon
          name="lucide:clock"
          class="text-info-600 mt-0.5 size-4 shrink-0"
        />
        <p class="text-info-700 text-sm">
          确认后只会开出一条合并提案，约 48
          小时冷静期后才会真正执行；期间可以在「合并提案」里撤回。
        </p>
      </div>

      <div
        v-if="releasesClaim"
        class="border-danger-200 bg-danger-50 flex items-start gap-2 rounded-lg border px-3 py-2"
      >
        <KunIcon
          name="lucide:shield-alert"
          class="text-danger mt-0.5 size-4 shrink-0"
        />
        <p class="text-danger-700 text-sm">
          两侧都被站点认领：合并会<span class="font-bold">释放</span
          >被并入一侧（{{ source?.site }}）的认领关系，该站点将失去这条{{
            entityNoun
          }}的所有权。执行后不可自动恢复。
        </p>
      </div>

      <KunInput v-model="note" placeholder="备注（可选，随提案携带）" />

      <div class="flex justify-end gap-3">
        <KunButton color="default" variant="flat" @click="open = false">
          取消
        </KunButton>
        <KunButton
          :color="releasesClaim ? 'danger' : 'success'"
          :disabled="busy"
          @click="emit('confirm', note)"
        >
          <KunIcon
            v-if="busy"
            name="lucide:loader-circle"
            class="mr-1 size-4 animate-spin"
          />
          确认合并并开提案
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>

<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { CatalogRevision } from '#shared/types/catalog'
import { REVISION_ACTION } from '~/constants/catalog'

const props = defineProps<{
  revision: CatalogRevision
  name?: string
  showEntity?: boolean
  selectable?: boolean
  selected?: boolean
  expanded?: boolean
  canRevert?: boolean
  isLatest?: boolean
}>()

const emit = defineEmits<{
  toggleSelect: []
  toggleDiff: []
  revert: []
}>()

const action = computed(
  () =>
    REVISION_ACTION[props.revision.action] ?? {
      label: props.revision.action,
      tone: 'bg-default-100 text-default-600',
      icon: 'lucide:circle'
    }
)
</script>

<template>
  <li class="py-3">
    <div class="flex items-start gap-3">
      <KunCheckBox
        v-if="props.selectable"
        :model-value="props.selected"
        label="对比"
        size="sm"
        class-name="mt-1 shrink-0"
        @update:model-value="emit('toggleSelect')"
      />
      <span
        :class="
          cn(
            'mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full',
            action.tone
          )
        "
      >
        <KunIcon :name="action.icon" class="size-3.5" />
      </span>
      <div class="min-w-0 flex-1">
        <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <CommonEntityLink
            v-if="props.showEntity"
            :family="props.revision.target_object"
            :id="props.revision.entity_id"
            :name="props.name ?? `#${props.revision.entity_id}`"
          />
          <span
            :class="
              props.showEntity
                ? 'text-default-400 text-xs'
                : 'text-foreground font-medium'
            "
          >
            第 {{ props.revision.seq }} 版 · {{ action.label }}
          </span>
          <KunChip
            v-if="props.isLatest"
            size="xs"
            variant="flat"
            color="primary"
            >当前</KunChip
          >
        </div>
        <div
          v-if="props.revision.changed_fields.length"
          class="mt-1 flex flex-wrap gap-1"
        >
          <KunChip
            v-for="field in props.revision.changed_fields"
            :key="field"
            size="xs"
            variant="flat"
          >
            {{ fieldLabel(field) }}
          </KunChip>
        </div>
        <p
          class="text-default-400 mt-1 flex flex-wrap items-center gap-x-1 text-xs"
        >
          <HistoryAuthor
            :user="props.revision.actor"
            :uid="props.revision.actor_uid"
            :origin="props.revision.origin_site"
            :site="props.revision.site"
          />
          <template v-if="props.revision.amender_uid">
            <span>· 修正</span>
            <HistoryUser
              :user="props.revision.amender"
              :uid="props.revision.amender_uid"
            />
          </template>
          <span>·</span>
          <time
            :datetime="props.revision.created_at"
            :title="formatDateTime(props.revision.created_at)"
            >{{ formatRelative(props.revision.created_at) }}</time
          >
          <NuxtLink
            v-if="props.revision.proposal_id"
            :to="`/review/${props.revision.proposal_id}`"
            class="hover:text-primary"
          >
            · 提案 #{{ props.revision.proposal_id }}
          </NuxtLink>
        </p>
      </div>
      <div class="flex shrink-0 flex-wrap justify-end gap-1">
        <KunButton size="sm" variant="light" @click="emit('toggleDiff')">
          {{ props.expanded ? '收起' : '查看改动' }}
        </KunButton>
        <KunButton
          v-if="props.showEntity"
          size="sm"
          variant="light"
          @click="
            navigateTo(
              `/history/${props.revision.target_object}/${props.revision.entity_id}`
            )
          "
        >
          历史
        </KunButton>
        <KunButton
          v-if="props.canRevert && !props.isLatest"
          size="sm"
          variant="light"
          color="warning"
          @click="emit('revert')"
        >
          回滚到此版
        </KunButton>
      </div>
    </div>
    <div v-if="props.expanded" class="mt-3 sm:pl-10">
      <slot />
    </div>
  </li>
</template>

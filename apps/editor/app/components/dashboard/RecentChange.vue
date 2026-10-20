<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { ActivityRevision } from '#shared/types/activity'
import { REVISION_ACTION, SITE_LABELS } from '~/constants/catalog'

const props = defineProps<{
  revision: ActivityRevision
  name: string
}>()

const action = computed(
  () =>
    REVISION_ACTION[props.revision.action] ?? {
      label: props.revision.action,
      tone: 'bg-default-100 text-default-600',
      icon: 'lucide:circle'
    }
)

const fields = computed(() =>
  (props.revision.changed_fields ?? []).map(fieldLabel)
)

const actorName = computed(
  () => props.revision.actor?.name || `用户 #${props.revision.actor_uid}`
)
</script>

<template>
  <li class="flex items-start gap-3 py-3">
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
          :family="props.revision.target_object"
          :id="props.revision.entity_id"
          :name="props.name"
        />
        <span class="text-default-400 text-xs">
          第 {{ props.revision.seq }} 版 · {{ action.label }}
        </span>
      </div>
      <div v-if="fields.length" class="mt-1 flex flex-wrap gap-1">
        <KunChip v-for="field in fields" :key="field" size="xs" variant="flat">
          {{ field }}
        </KunChip>
      </div>
      <p class="text-default-400 mt-1 flex items-center gap-1 text-xs">
        <KunAvatar
          v-if="props.revision.actor?.avatar"
          :user="{
            id: 0,
            name: actorName,
            avatar: props.revision.actor.avatar
          }"
          size="xs"
        />
        <span class="text-default-600">{{ actorName }}</span>
        · {{ labelOf(SITE_LABELS, props.revision.site) }} ·
        <time
          :datetime="props.revision.created_at"
          :title="formatDateTime(props.revision.created_at)"
        >
          {{ formatRelative(props.revision.created_at) }}
        </time>
      </p>
    </div>
  </li>
</template>

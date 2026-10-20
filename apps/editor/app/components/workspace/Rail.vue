<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { WorkspaceGroupEntry } from '~/composables/useWorkspaceSections'
import { ENTITY_META, type EditFamily } from '~/constants/editEntities'

const props = withDefaults(
  defineProps<{
    family: EditFamily
    name: string
    thumbnail?: { url: string; thumbhash?: string | null } | null
    groups: WorkspaceGroupEntry[]
    changes: Record<string, number>
    counts?: Record<string, number | null>
    loading?: boolean
    collapsed?: boolean
  }>(),
  { thumbnail: null, counts: () => ({}), loading: false, collapsed: false }
)

const current = defineModel<string>({ required: true })
const emit = defineEmits<{ 'toggle-collapse': [] }>()

const meta = computed(() => ENTITY_META[props.family])
</script>

<template>
  <nav
    class="flex h-full min-h-0 flex-col"
    :aria-label="`${meta.label}信息分区`"
  >
    <div :class="cn('shrink-0 px-2 pt-2', props.collapsed && 'px-1.5')">
      <WorkspaceHint
        :text="props.collapsed ? '返回工作台' : ''"
        position="right"
      >
        <NuxtLink
          to="/"
          :class="
            cn(
              'text-default-600 hover:bg-default-100 hover:text-foreground flex h-9 items-center gap-2 rounded-lg px-2.5 text-sm transition-colors',
              props.collapsed && 'justify-center px-0'
            )
          "
        >
          <KunIcon name="lucide:arrow-left" class="size-4 shrink-0" />
          <span v-if="!props.collapsed">返回工作台</span>
        </NuxtLink>
      </WorkspaceHint>
    </div>

    <div
      v-if="!props.collapsed"
      class="border-default-200 mx-3 mt-2 flex items-center gap-3 border-b pb-3"
    >
      <div
        class="bg-default-100 text-default-400 flex size-11 shrink-0 items-center justify-center overflow-hidden rounded-lg"
      >
        <KunImage
          v-if="props.thumbnail?.url"
          :src="props.thumbnail.url"
          :thumbhash="props.thumbnail.thumbhash ?? undefined"
          alt=""
          aspect-ratio="1 / 1"
          object-fit="cover"
          class-name="size-11"
        />
        <KunIcon v-else :name="meta.icon" class="size-5" />
      </div>
      <div class="min-w-0">
        <p class="line-clamp-2 text-sm leading-5 font-semibold">
          {{ props.name }}
        </p>
        <p class="text-default-500 text-xs">{{ meta.label }}</p>
      </div>
    </div>

    <div class="min-h-0 flex-1 overflow-y-auto">
      <WorkspaceSkeletonRail v-if="props.loading" />
      <div
        v-else
        :class="cn('space-y-4 py-3', props.collapsed ? 'px-1.5' : 'px-2')"
      >
        <div v-for="entry in props.groups" :key="entry.group">
          <p
            v-if="!props.collapsed"
            class="text-default-400 px-2.5 pb-1 text-xs font-medium"
          >
            {{ entry.label }}
          </p>
          <KunDivider v-else class-name="mx-1 mb-2 w-auto" />
          <ul class="space-y-0.5">
            <li v-for="section in entry.sections" :key="section.key">
              <WorkspaceHint
                :text="props.collapsed ? section.label : ''"
                position="right"
              >
                <button
                  type="button"
                  :aria-current="current === section.key ? 'page' : undefined"
                  :class="
                    cn(
                      'flex h-9 w-full items-center gap-2.5 rounded-lg px-2.5 text-left text-sm transition-colors',
                      current === section.key
                        ? 'bg-primary-50 text-primary-700 font-medium'
                        : 'text-default-600 hover:bg-default-100 hover:text-foreground',
                      props.collapsed && 'justify-center px-0'
                    )
                  "
                  @click="current = section.key"
                >
                  <span class="relative flex shrink-0">
                    <KunIcon :name="section.icon" class="size-4" />
                    <span
                      v-if="props.collapsed && props.changes[section.key]"
                      class="bg-primary-500 absolute -top-1 -right-1 size-2 rounded-full"
                    />
                  </span>
                  <template v-if="!props.collapsed">
                    <span class="min-w-0 flex-1 truncate">
                      {{ section.label }}
                    </span>
                    <span
                      v-if="props.changes[section.key]"
                      class="bg-primary-100 text-primary-700 rounded-full px-1.5 text-xs leading-5 tabular-nums"
                    >
                      {{ props.changes[section.key] }}
                    </span>
                    <span
                      v-else-if="props.counts[section.key] != null"
                      class="text-default-400 text-xs tabular-nums"
                    >
                      {{ props.counts[section.key] }}
                    </span>
                  </template>
                </button>
              </WorkspaceHint>
            </li>
          </ul>
        </div>
      </div>
    </div>

    <div
      :class="
        cn(
          'border-default-200 flex shrink-0 items-center gap-1 border-t p-2',
          props.collapsed && 'flex-col px-1.5'
        )
      "
    >
      <WorkspaceHint :text="props.collapsed ? '编辑规范' : ''" position="right">
        <a
          href="/guidelines"
          target="_blank"
          rel="noopener"
          :class="
            cn(
              'text-default-600 hover:bg-default-100 hover:text-foreground flex h-9 flex-1 items-center gap-2 rounded-lg px-2.5 text-sm transition-colors',
              props.collapsed && 'w-full flex-none justify-center px-0'
            )
          "
        >
          <KunIcon name="lucide:book-open" class="size-4 shrink-0" />
          <span v-if="!props.collapsed">编辑规范</span>
        </a>
      </WorkspaceHint>
      <button
        type="button"
        :aria-label="props.collapsed ? '展开侧边栏' : '折叠侧边栏'"
        class="text-default-500 hover:bg-default-100 hover:text-foreground hidden size-9 shrink-0 items-center justify-center rounded-lg transition-colors lg:flex"
        @click="emit('toggle-collapse')"
      >
        <KunIcon
          :name="
            props.collapsed
              ? 'lucide:panel-left-open'
              : 'lucide:panel-left-close'
          "
          class="size-4"
        />
      </button>
    </div>
  </nav>
</template>

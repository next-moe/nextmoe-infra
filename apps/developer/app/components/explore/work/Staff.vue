<script setup lang="ts">
import {
  EXPLORE_STAFF_LEAD_ROLES,
  EXPLORE_STAFF_TAIL_ROLES
} from '~/constants/explore'
import type {
  ExploreCreditName,
  ExploreEntityTarget,
  ExploreWork
} from '~~/shared/types/explore'

const props = defineProps<{ work: ExploreWork }>()
const emit = defineEmits<{ open: [target: ExploreEntityTarget] }>()

const VOICE_ROLE = 'voice-actor'

const characterName = computed(
  () =>
    new Map((props.work.characters ?? []).map((c) => [c.id, c.display_name]))
)

const cast = computed(() => {
  const by = new Map<string, ExploreCreditName[]>()
  const unassigned: ExploreCreditName[] = []
  for (const g of props.work.credits ?? []) {
    if (g.role_key !== VOICE_ROLE) continue
    for (const c of g.credits) {
      if (!c.character_id) {
        if (!unassigned.some((x) => x.id === c.id)) unassigned.push(c)
        continue
      }
      by.set(c.character_id, [...(by.get(c.character_id) ?? []), c])
    }
  }
  const rows = [...by.entries()]
    .map(([id, voices]) => ({ id, name: characterName.value.get(id), voices }))
    .sort((a, b) => Number(!a.name) - Number(!b.name))
  return { rows, unassigned }
})

const rank = (role: string): number => {
  const lead = EXPLORE_STAFF_LEAD_ROLES.indexOf(role)
  if (lead >= 0) return lead
  const tail = EXPLORE_STAFF_TAIL_ROLES.indexOf(role)
  return tail >= 0 ? 1000 + tail : 500
}

const staff = computed(() => {
  const by = new Map<string, ExploreCreditName[]>()
  for (const g of props.work.credits ?? []) {
    if (g.role_key === VOICE_ROLE) continue
    const list = by.get(g.role_name) ?? []
    for (const c of g.credits)
      if (!list.some((x) => x.id === c.id)) list.push(c)
    by.set(g.role_name, list)
  }
  return [...by.entries()]
    .map(([role, names]) => ({ role, names }))
    .sort((a, b) => rank(a.role) - rank(b.role))
})
</script>

<template>
  <div class="space-y-8">
    <section v-if="staff.length" class="space-y-3">
      <h3 class="text-foreground text-base font-semibold">制作阵容</h3>
      <KunCard padding="none" class-name="overflow-hidden">
        <dl class="divide-default-100 divide-y text-sm">
          <div
            v-for="s in staff"
            :key="s.role"
            class="grid grid-cols-1 gap-1 px-4 py-2.5 sm:grid-cols-[8rem_minmax(0,1fr)] sm:gap-4"
          >
            <dt class="text-default-400">{{ s.role }}</dt>
            <dd class="flex flex-wrap gap-x-3 gap-y-1">
              <button
                v-for="n in s.names"
                :key="n.id"
                type="button"
                class="text-foreground hover:text-primary hover:underline"
                @click="emit('open', { kind: 'credit-names', id: n.id })"
              >
                {{ n.display_name }}
              </button>
            </dd>
          </div>
        </dl>
      </KunCard>
    </section>

    <section
      v-if="cast.rows.length || cast.unassigned.length"
      class="space-y-3"
    >
      <h3 class="text-foreground text-base font-semibold">声优</h3>
      <div class="grid grid-cols-1 gap-2 md:grid-cols-2">
        <div
          v-for="r in cast.rows"
          :key="r.id"
          class="border-default-200 bg-content1 flex items-center gap-3 rounded-xl border px-4 py-2.5 text-sm"
        >
          <button
            type="button"
            class="text-foreground hover:text-primary min-w-0 flex-1 truncate text-left font-medium"
            @click="emit('open', { kind: 'characters', id: r.id })"
          >
            {{ r.name ?? `角色 #${r.id}` }}
          </button>
          <span
            class="text-default-500 flex shrink-0 flex-wrap justify-end gap-x-2"
          >
            <button
              v-for="v in r.voices"
              :key="v.id"
              type="button"
              class="hover:text-primary inline-flex items-center gap-1 hover:underline"
              @click="emit('open', { kind: 'credit-names', id: v.id })"
            >
              <KunIcon name="lucide:mic" class="text-default-400 size-3.5" />
              {{ v.display_name }}
            </button>
          </span>
        </div>
      </div>
      <p
        v-if="cast.unassigned.length"
        class="text-default-500 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm"
      >
        <span class="text-default-400">未标注角色</span>
        <button
          v-for="v in cast.unassigned"
          :key="v.id"
          type="button"
          class="hover:text-primary hover:underline"
          @click="emit('open', { kind: 'credit-names', id: v.id })"
        >
          {{ v.display_name }}
        </button>
      </p>
    </section>
  </div>
</template>

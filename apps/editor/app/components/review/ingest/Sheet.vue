<script setup lang="ts">
import type { CanonTodoSheet } from '#shared/types/canon'
import { CANON_ENTITY_LABELS } from '~/constants/canon'
import { answerOptions, candidateOrigin, showValue } from '~/utils/canon/todo'

const props = defineProps<{ sheet: CanonTodoSheet; editable: boolean }>()
const chosen = defineModel<Record<string, string>>('chosen', { required: true })
const omitted = defineModel<string[]>('omitted', { required: true })

const entityLabel = (entity: string) => CANON_ENTITY_LABELS[entity] ?? entity

const choose = (ref: string, value: string) => {
  chosen.value = { ...chosen.value, [ref]: value }
}

const omit = (ref: string, on: boolean) => {
  omitted.value = on
    ? [...omitted.value, ref]
    : omitted.value.filter((r) => r !== ref)
}
</script>

<template>
  <div class="space-y-3">
    <div
      v-if="props.sheet.notes?.length"
      class="border-default-200 space-y-1 rounded-lg border px-3 py-2"
    >
      <p
        v-for="n in props.sheet.notes"
        :key="n.at"
        class="text-default-600 text-sm"
      >
        <span class="text-default-400">{{ n.by }}：</span>{{ n.note }}
      </p>
    </div>

    <div
      v-for="q in props.sheet.questions"
      :key="q.ref"
      class="border-warning-300 space-y-2 rounded-lg border px-3 py-2"
    >
      <p class="text-foreground text-sm font-medium">
        {{ entityLabel(q.entity) }}「{{ q.name }}」是哪一个？
        <span class="text-default-400 ml-1 font-mono text-xs">{{ q.ref }}</span>
      </p>
      <ul v-if="q.candidates.length" class="space-y-1">
        <li v-for="c in q.candidates" :key="c.token" class="text-sm">
          <span class="text-default-500">
            {{ candidateOrigin(c) }} #{{ c.id }}
          </span>
          {{ c.label }}
          <span v-if="c.facts.length" class="text-default-400 text-xs">
            — {{ c.facts.join('；') }}
          </span>
        </li>
      </ul>
      <p v-else class="text-default-500 text-sm">没有同名的条目。</p>
      <KunRadioGroup
        v-if="props.editable"
        :model-value="chosen[q.ref] ?? ''"
        :options="answerOptions(q)"
        size="sm"
        :aria-label="`${q.ref} 是哪一个`"
        @update:model-value="(v: string | number) => choose(q.ref, String(v))"
      />
    </div>

    <div class="border-default-200 divide-default-200 divide-y rounded-lg border">
      <div
        v-for="change in props.sheet.changes"
        :key="change.ref || `${change.entity}-${change.id}`"
        class="space-y-1 px-3 py-2"
        :class="omitted.includes(change.ref) && 'opacity-40'"
      >
        <div class="flex items-center justify-between gap-2">
          <p class="text-sm">
            <span class="text-foreground font-medium">{{
              entityLabel(change.entity)
            }}</span>
            <span class="text-default-400 ml-2 font-mono text-xs">{{
              change.ref || `#${change.id}`
            }}</span>
          </p>
          <KunCheckBox
            v-if="props.editable && change.ref"
            :model-value="omitted.includes(change.ref)"
            label="不写入"
            size="sm"
            class-name="shrink-0"
            @update:model-value="(on: boolean) => omit(change.ref, on)"
          />
        </div>
        <dl class="grid grid-cols-[8rem_1fr] gap-x-3 gap-y-0.5 text-sm">
          <template v-for="v in change.values ?? []" :key="v.property">
            <dt class="text-default-500 font-mono text-xs leading-5">
              {{ v.property }}
            </dt>
            <dd class="text-foreground break-all">
              {{ showValue(v) }}
              <span v-if="v.tier" class="text-default-400 text-xs">
                · {{ v.tier }}
              </span>
              <p
                v-for="e in v.evidence ?? []"
                :key="e"
                class="text-default-400 text-xs"
              >
                {{ e }}
              </p>
            </dd>
          </template>
        </dl>
      </div>
    </div>
  </div>
</template>

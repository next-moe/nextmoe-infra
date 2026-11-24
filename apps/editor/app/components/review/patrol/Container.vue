<script setup lang="ts">
import type { CanonPage, CanonPatrolItem } from '#shared/types/canon'
import { CANON_ACTIONS, CANON_ENTITY_LABELS } from '~/constants/canon'
import { canonMessage } from '~/utils/canon/record'
import { formatRelative } from '~/utils/format'
import { problemStatus } from '~/utils/review/problem'

useHead({ title: '采集巡查 · 未萌编辑台' })

const api = useEditorApi()
const items = ref<CanonPatrolItem[]>([])
const next = ref<string | null>(null)
const loading = ref(true)
const forbidden = ref(false)
const busy = ref('')

const load = async (cursor: string | null) => {
  loading.value = true
  try {
    const page = await api<CanonPage<CanonPatrolItem>>('/v3/catalog/patrol', {
      query: { limit: 20, ...(cursor ? { cursor } : {}) }
    })
    items.value = cursor ? [...items.value, ...page.items] : page.items
    next.value = page.next_cursor ?? null
    forbidden.value = false
  } catch (error) {
    forbidden.value = problemStatus(error) === 403
    if (!forbidden.value) {
      useKunMessage(canonMessage(error), 'error')
    }
  } finally {
    loading.value = false
  }
}

const act = async (item: CanonPatrolItem, action: 'patrol' | 'undo') => {
  busy.value = item.batch
  try {
    if (action === 'patrol') {
      await api(`/v3/catalog/patrol/${item.batch}`, { method: 'POST' })
    } else {
      const ok = await useKunAlert({
        title: '撤回这一次写入？',
        message: '被撤回的内容不会自动写回；占比太高时程序会自动停。'
      })
      if (!ok) {
        return
      }
      await api(`/v3/catalog/commits/${item.batch}/undo`, {
        method: 'POST',
        body: { note: '巡查撤回' }
      })
      await api(`/v3/catalog/patrol/${item.batch}`, { method: 'POST' })
    }
    items.value = items.value.filter((i) => i.batch !== item.batch)
  } catch (error) {
    useKunMessage(canonMessage(error), 'error')
  } finally {
    busy.value = ''
  }
}

const rootLink = (item: CanonPatrolItem) => {
  const work = item.revisions.find((r) => r.entity === 'work')
  const release = item.revisions.find((r) => r.entity === 'release')
  const target = work ?? release
  return target ? `/canon/${target.entity}/${target.entity_id}` : ''
}

const valueText = (value: unknown) => {
  const v = (value as { value?: unknown } | null)?.value
  return typeof v === 'string' ? v : JSON.stringify(v)
}

onMounted(() => load(null))
</script>

<template>
  <div class="mx-auto max-w-5xl space-y-5">
    <KunHeader
      name="采集巡查"
      description="采集程序每一次写入都要有人看过。看过只做标记，不改任何值；写错了就撤回。"
    />
    <KunInfo
      v-if="forbidden"
      color="warning"
      variant="flat"
      title="需要采集巡查权限"
      description="巡查需要 canon.patrol，改程序模式需要 canon.ingest_manage。"
    />
    <template v-else>
      <ReviewPatrolPrograms />
      <CommonSkeletonRows v-if="loading && !items.length" :rows="4" />
      <KunCard v-for="item in items" :key="item.batch" bordered>
        <div class="space-y-2">
          <div class="flex items-center gap-2">
            <NuxtLink
              v-if="rootLink(item)"
              :to="rootLink(item)"
              class="text-primary-600 text-sm"
            >
              打开条目
            </NuxtLink>
            <span class="text-default-400 text-xs">
              {{ item.program }}
              <template v-if="item.reviewed_by"> · 审核 {{ item.reviewed_by }}</template>
              · {{ formatRelative(item.at) }}
            </span>
            <div class="flex-1" />
            <KunButton
              size="sm"
              variant="light"
              color="danger"
              :loading="busy === item.batch"
              @click="act(item, 'undo')"
            >
              撤回
            </KunButton>
            <KunButton
              size="sm"
              color="primary"
              :loading="busy === item.batch"
              @click="act(item, 'patrol')"
            >
              看过了
            </KunButton>
          </div>
          <ul class="space-y-1">
            <li v-for="r in item.revisions" :key="r.id" class="text-sm">
              <span class="text-foreground">
                {{ CANON_ACTIONS[r.action] ?? r.action }}
                {{ CANON_ENTITY_LABELS[r.entity] ?? r.entity }} #{{ r.entity_id }}
              </span>
              <span
                v-for="(value, key) in r.changes"
                :key="key"
                class="text-default-500 ml-2"
              >
                {{ key }}={{ valueText(value) }}
              </span>
            </li>
          </ul>
        </div>
      </KunCard>
      <KunNull v-if="!loading && !items.length" description="没有待巡查的写入" />
      <div v-if="next" class="flex justify-center">
        <KunButton variant="bordered" :loading="loading" @click="load(next)">
          加载更多
        </KunButton>
      </div>
    </template>
  </div>
</template>

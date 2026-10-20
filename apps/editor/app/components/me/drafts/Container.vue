<script setup lang="ts">
import type { LocalDraft } from '#shared/types/activity'
import { ENTITY_FAMILY } from '~/constants/catalog'

useHead({ title: '我的草稿 · 未萌编辑台' })

const names = useEntityNames()
const drafts = ref<LocalDraft[]>([])
const ready = ref(false)

const reload = () => {
  try {
    drafts.value = readLocalDrafts(localStorage)
  } catch {
    drafts.value = []
  }
  names.resolve(
    drafts.value.map((draft) => ({
      target_object: draft.family,
      entity_id: draft.entityId
    }))
  )
  ready.value = true
}

onMounted(reload)

const discard = async (draft: LocalDraft) => {
  const ok = await useKunAlert({
    title: '丢弃这份草稿？',
    message: '草稿只保存在这个浏览器里，丢弃后无法恢复。',
    type: 'warning',
    confirmText: '丢弃',
    cancelText: '取消'
  })
  if (!ok) {
    return
  }
  try {
    localStorage.removeItem(draft.key)
  } catch {
    useKunMessage('浏览器拒绝了本地存储操作', 'error')
    return
  }
  reload()
}

const familyLabel = (family: string) => ENTITY_FAMILY[family]?.label ?? family
</script>

<template>
  <div class="mx-auto max-w-4xl space-y-6">
    <KunHeader
      name="我的草稿"
      description="编辑页会自动保存尚未提交的修改。草稿只保存在当前浏览器里，换设备或清理浏览器数据后会丢失。"
    />

    <KunLoading v-if="!ready" spinner />
    <KunCard v-else-if="drafts.length" bordered padding="none">
      <ul class="divide-default-200 divide-y">
        <li
          v-for="draft in drafts"
          :key="draft.key"
          class="flex flex-col gap-3 p-4 sm:flex-row sm:items-center"
        >
          <div class="min-w-0 flex-1">
            <div class="flex min-w-0 items-center gap-2">
              <span class="text-default-400 shrink-0 text-xs">
                {{ familyLabel(draft.family) }}
              </span>
              <span class="truncate font-medium">
                {{
                  names.nameOf({
                    target_object: draft.family,
                    entity_id: draft.entityId
                  })
                }}
              </span>
            </div>
            <div class="mt-1 flex flex-wrap gap-1">
              <KunChip
                v-for="key in draft.fieldKeys"
                :key="key"
                size="xs"
                variant="flat"
              >
                {{ fieldLabel(key) }}
              </KunChip>
            </div>
            <p class="text-default-400 mt-1 text-xs">
              保存于
              <time
                :datetime="draft.savedAt"
                :title="formatDateTime(draft.savedAt)"
              >
                {{ formatRelative(draft.savedAt) }}
              </time>
            </p>
          </div>
          <div class="flex shrink-0 gap-2">
            <KunButton
              v-if="draftEditPath(draft)"
              size="sm"
              color="primary"
              variant="flat"
              @click="navigateTo(draftEditPath(draft)!)"
            >
              继续编辑
            </KunButton>
            <KunButton
              size="sm"
              variant="light"
              color="danger"
              @click="discard(draft)"
            >
              丢弃
            </KunButton>
          </div>
        </li>
      </ul>
    </KunCard>
    <KunNull v-else description="没有未提交的草稿" :is-show-sticker="false" />
  </div>
</template>

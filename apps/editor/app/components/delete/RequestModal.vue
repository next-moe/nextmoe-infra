<script setup lang="ts">
import type { KunRadioOption } from '@kungal/ui-vue'
import type {
  CatalogDeletionPreview,
  CatalogDeletionRequest
} from '#shared/types/catalog'
import { DELETION_REASONS } from '~/constants/deletion'
import { newIdempotencyKey, problemMessage } from '~/utils/review/problem'

const props = defineProps<{ workId: string; workName: string }>()
const open = defineModel<boolean>({ required: true })
const emit = defineEmits<{ submitted: [request: CatalogDeletionRequest] }>()

const NOTE_HINTS: Record<string, string> = {
  not_a_work: '写出你查过的地方，例如：官网、DLsite、批评空间、VNDB 都查不到这部作品。',
  out_of_scope: '写出它是什么、为什么不属于收录范围，例如：这是一部动画，不是视觉小说。',
  spam_or_test: '写出判断依据，例如：标题是乱码、简介是广告。',
  other: '写清楚为什么要删除，以及审核者可以怎样核实。'
}

const api = useEditorApi()
const reason = ref<string>('not_a_work')
const note = ref('')
const submitting = ref(false)
const preview = ref<CatalogDeletionPreview | null>(null)
const previewLoading = ref(false)

const options = computed<KunRadioOption<string>[]>(() =>
  DELETION_REASONS.map((r) => ({
    value: r.value,
    label: r.label,
    description: r.description
  }))
)

const isDuplicate = computed(() => reason.value === 'duplicate')
const pending = computed(() => preview.value?.pending_request_id ?? null)
const canSubmit = computed(
  () =>
    !isDuplicate.value &&
    !pending.value &&
    note.value.trim().length >= 4 &&
    !submitting.value
)

const loadPreview = async () => {
  previewLoading.value = true
  try {
    preview.value = await api<CatalogDeletionPreview>(
      '/v2/me/deletion-preview',
      { query: { object: 'work', target_id: props.workId } }
    )
  } catch (error) {
    useKunMessage(problemMessage(error, '影响范围加载失败'), 'warn')
  } finally {
    previewLoading.value = false
  }
}

watch(open, (value) => {
  if (value) {
    reason.value = 'not_a_work'
    note.value = ''
    preview.value = null
    loadPreview()
  }
})

const submit = async () => {
  if (!canSubmit.value) {
    return
  }
  submitting.value = true
  try {
    const request = await api<CatalogDeletionRequest>(
      '/v2/me/deletion-requests',
      {
        method: 'POST',
        headers: { 'Idempotency-Key': newIdempotencyKey() },
        body: {
          object: 'work',
          target_id: props.workId,
          reason: reason.value,
          note: note.value.trim()
        }
      }
    )
    useKunMessage('删除请求已提交，审核通过并度过冷静期后才会删除', 'success')
    open.value = false
    emit('submitted', request)
  } catch (error) {
    useKunMessage(problemMessage(error, '提交失败'), 'error')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    title="申请删除作品"
    :description="`「${props.workName}」会在审核通过、度过冷静期后删除；删除后可以原样恢复。`"
    size="xl"
  >
    <div class="space-y-5">
      <section class="space-y-2">
        <h3 class="text-sm font-medium">为什么要删除</h3>
        <KunRadioGroup
          v-model="reason"
          :options="options"
          variant="card"
          orientation="vertical"
          aria-label="删除原因"
        />
      </section>

      <KunInfo
        v-if="isDuplicate"
        color="primary"
        variant="flat"
        title="重复的条目不删除，而是合并"
        description="合并会把两边的资料、评分和收藏归到保留的那一个，旧编号继续跳转过去。"
      >
        <KunButton
          :href="`/merge/new?source=${props.workId}`"
          target="_blank"
          color="primary"
          size="sm"
          class-name="mt-2"
        >
          <KunIcon name="lucide:git-merge" class="size-4" />
          改为发起合并
        </KunButton>
      </KunInfo>

      <template v-else>
        <KunInfo
          v-if="pending"
          color="warning"
          variant="flat"
          title="这部作品已有一个待处理的删除请求"
          description="同一部作品同时只能有一个删除请求，可以在请求下补充说明。"
        />

        <section class="space-y-2">
          <h3 class="text-sm font-medium">删除会影响什么</h3>
          <DeleteImpact :preview="preview" :loading="previewLoading" />
        </section>

        <section class="space-y-2">
          <h3 class="text-sm font-medium">
            说明
            <span class="text-default-400 font-normal">（审核者会看到）</span>
          </h3>
          <KunTextarea
            v-model="note"
            :placeholder="NOTE_HINTS[reason] ?? ''"
            aria-label="删除说明"
            auto-grow
            max-height="240px"
          />
        </section>
      </template>

      <div class="flex items-center justify-between gap-3">
        <NuxtLink
          to="/guidelines/create-and-merge#deleting"
          target="_blank"
          class="text-default-500 hover:text-primary inline-flex items-center gap-1 text-xs"
        >
          <KunIcon name="lucide:book-open" class="size-3.5" />
          什么时候该删除
        </NuxtLink>
        <div class="flex gap-2">
          <KunButton variant="light" @click="open = false">取消</KunButton>
          <KunButton
            v-if="!isDuplicate"
            color="danger"
            :disabled="!canSubmit"
            :loading="submitting"
            @click="submit"
          >
            提交删除请求
          </KunButton>
        </div>
      </div>
    </div>
  </KunModal>
</template>

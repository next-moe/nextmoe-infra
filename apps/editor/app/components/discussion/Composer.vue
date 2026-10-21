<script setup lang="ts">
import type { KunDropdownItem } from '@kungal/ui-vue'
import type { DiscussionPost } from '#shared/types/discussion'
import type { DiscussionDraft } from '~/composables/useDiscussion'
import {
  DISCUSSION_MAX_BODY,
  DISCUSSION_MAX_MENTIONS
} from '~/constants/discussion'

interface MentionCandidate {
  uid: string
  name: string
  role?: string
}

const props = defineProps<{
  candidates: MentionCandidate[]
  replyTo: DiscussionPost | null
  placeholder: string
}>()

const emit = defineEmits<{
  submit: [draft: DiscussionDraft]
  cancelReply: []
}>()

const body = ref('')
const wrapper = ref<HTMLElement | null>(null)

const trimmed = computed(() => body.value.trim())
const mentioned = computed(() =>
  mentionsInBody(body.value, props.candidates).slice(0, DISCUSSION_MAX_MENTIONS)
)

const focusField = () => {
  nextTick(() => wrapper.value?.querySelector('textarea')?.focus())
}

const insertMention = (candidate: MentionCandidate) => {
  const token = `@${candidate.name} `
  if (!body.value.includes(token.trim())) {
    body.value =
      body.value && !/\s$/.test(body.value)
        ? `${body.value} ${token}`
        : `${body.value}${token}`
  }
  focusField()
}

const mentionItems = computed(() =>
  props.candidates.map((candidate) => ({
    key: candidate.uid,
    label: candidate.role
      ? `@${candidate.name}（${candidate.role}）`
      : `@${candidate.name}`,
    icon: 'lucide:at-sign'
  }))
)

const onMention = (item: KunDropdownItem) => {
  const candidate = props.candidates.find((entry) => entry.uid === item.key)
  if (candidate) {
    insertMention(candidate)
  }
}

const submit = () => {
  if (!trimmed.value) {
    return
  }
  emit('submit', {
    body: trimmed.value,
    replyToId: props.replyTo?.id ?? null,
    mentionUids: mentioned.value
  })
  body.value = ''
}

const onKey = (event: KeyboardEvent) => {
  if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) {
    event.preventDefault()
    submit()
  }
}

watch(
  () => props.replyTo?.id,
  (id) => id && focusField()
)

defineExpose({ focus: focusField })
</script>

<template>
  <div ref="wrapper" class="space-y-2">
    <div
      v-if="props.replyTo"
      class="bg-default-50 text-default-600 flex items-start gap-2 rounded-lg px-3 py-2 text-xs"
    >
      <KunIcon name="lucide:reply" class="mt-0.5 size-3.5 shrink-0" />
      <p class="min-w-0 flex-1">
        回复
        {{
          props.replyTo.author?.name ?? `用户 #${props.replyTo.author_uid}`
        }}：{{ discussionSnippet(props.replyTo.body, 80) }}
      </p>
      <KunButton
        size="xs"
        variant="light"
        is-icon-only
        aria-label="取消回复"
        @click="emit('cancelReply')"
      >
        <KunIcon name="lucide:x" class="size-3.5" />
      </KunButton>
    </div>

    <KunTextarea
      v-model="body"
      :placeholder="props.placeholder"
      aria-label="写留言"
      auto-grow
      :rows="3"
      max-height="320px"
      :maxlength="DISCUSSION_MAX_BODY"
      show-char-count
      @keydown="onKey"
    />

    <div class="flex flex-wrap items-center gap-2">
      <KunDropdown
        v-if="mentionItems.length"
        :items="mentionItems"
        position="top-start"
        @select="onMention"
      >
        <template #trigger>
          <KunButton size="sm" variant="light">
            <KunIcon name="lucide:at-sign" class="size-4" />
            提及
          </KunButton>
        </template>
      </KunDropdown>
      <p v-if="mentioned.length" class="text-default-400 text-xs">
        将通知 {{ mentioned.length }} 人
      </p>
      <span class="text-default-400 ml-auto hidden text-xs sm:inline">
        <KunKbd keys="Mod+Enter" /> 发送
      </span>
      <KunButton size="sm" color="primary" :disabled="!trimmed" @click="submit">
        <KunIcon name="lucide:send" class="size-4" />
        发送
      </KunButton>
    </div>
  </div>
</template>

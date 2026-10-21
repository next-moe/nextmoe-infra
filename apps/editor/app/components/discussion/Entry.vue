<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import type { DiscussionPost } from '#shared/types/discussion'
import { DISCUSSION_MAX_BODY } from '~/constants/discussion'

const props = defineProps<{
  post: DiscussionPost
  replyTo?: DiscussionPost | null
  ownerUid: string | null
  ownerLabel: string
  mine: boolean
  canModerate: boolean
  canReply: boolean
}>()

const emit = defineEmits<{
  reply: [post: DiscussionPost]
  edit: [body: string, done: (ok: boolean) => void]
  remove: []
  moderate: [hidden: boolean, note: string, done: (ok: boolean) => void]
}>()

const editing = ref(false)
const editBody = ref('')
const saving = ref(false)
const hiding = ref(false)
const hideNote = ref('')

const visible = computed(() => props.post.state === 'visible')
const hidden = computed(() => props.post.state === 'hidden')
const deleted = computed(() => props.post.state === 'deleted')
const html = computed(() => renderDiscussionBody(props.post.body))
const isOwner = computed(
  () => !!props.ownerUid && props.post.author_uid === props.ownerUid
)

const startEdit = () => {
  editBody.value = props.post.body
  editing.value = true
}

const saveEdit = () => {
  const body = editBody.value.trim()
  if (!body || saving.value) {
    return
  }
  saving.value = true
  emit('edit', body, (ok) => {
    saving.value = false
    if (ok) {
      editing.value = false
    }
  })
}

const confirmRemove = async () => {
  const ok = await useKunAlert({
    title: '删除这条留言',
    message: '删除后留言内容会被清空，只留下“已删除”的占位。',
    type: 'warning',
    confirmText: '删除',
    cancelText: '取消'
  })
  if (ok) {
    emit('remove')
  }
}

const confirmHide = () => {
  saving.value = true
  emit('moderate', true, hideNote.value.trim(), (ok) => {
    saving.value = false
    if (ok) {
      hiding.value = false
      hideNote.value = ''
    }
  })
}

const restore = () => {
  emit('moderate', false, '', () => {})
}

const onEditKey = (event: KeyboardEvent) => {
  if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) {
    event.preventDefault()
    saveEdit()
  }
}
</script>

<template>
  <article
    :id="`discussion-post-${props.post.id}`"
    :class="
      cn(
        'group rounded-lg px-3 py-2.5 transition-colors',
        hidden && props.canModerate && 'bg-warning-50',
        !hidden && 'hover:bg-default-50'
      )
    "
  >
    <header class="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs">
      <HistoryUser
        :user="props.post.author"
        :uid="props.post.author_uid"
        size="sm"
      />
      <KunChip
        v-if="props.post.author_reviewer"
        size="sm"
        variant="flat"
        color="primary"
      >
        审核者
      </KunChip>
      <KunChip v-if="isOwner" size="sm" variant="flat" color="default">
        {{ props.ownerLabel }}
      </KunChip>
      <time
        class="text-default-400"
        :datetime="props.post.created_at"
        :title="formatDateTime(props.post.created_at)"
      >
        {{ formatRelative(props.post.created_at) }}
      </time>
      <span
        v-if="props.post.edited_at && visible"
        class="text-default-400"
        :title="`编辑于 ${formatDateTime(props.post.edited_at)}`"
      >
        · 已编辑
      </span>
      <div
        v-if="!editing && !hiding && !deleted"
        class="text-default-500 ml-auto flex flex-wrap gap-0.5 opacity-100 transition-opacity lg:opacity-0 lg:group-focus-within:opacity-100 lg:group-hover:opacity-100"
      >
        <KunButton
          v-if="props.canReply && visible"
          size="xs"
          variant="light"
          @click="emit('reply', props.post)"
        >
          <KunIcon name="lucide:reply" class="size-3.5" />
          回复
        </KunButton>
        <template v-if="props.mine && visible">
          <KunButton size="xs" variant="light" @click="startEdit">
            <KunIcon name="lucide:pencil" class="size-3.5" />
            编辑
          </KunButton>
          <KunButton
            size="xs"
            variant="light"
            color="danger"
            @click="confirmRemove"
          >
            <KunIcon name="lucide:trash-2" class="size-3.5" />
            删除
          </KunButton>
        </template>
        <template v-if="props.canModerate">
          <KunButton
            v-if="visible"
            size="xs"
            variant="light"
            color="warning"
            @click="hiding = true"
          >
            <KunIcon name="lucide:eye-off" class="size-3.5" />
            隐藏
          </KunButton>
          <KunButton v-else size="xs" variant="light" @click="restore">
            <KunIcon name="lucide:eye" class="size-3.5" />
            恢复显示
          </KunButton>
        </template>
      </div>
    </header>

    <a
      v-if="props.replyTo"
      :href="`#discussion-post-${props.replyTo.id}`"
      class="border-default-200 text-default-500 hover:text-foreground mt-2 block border-l-2 pl-2 text-xs"
    >
      回复
      {{ props.replyTo.author?.name ?? `用户 #${props.replyTo.author_uid}` }}：
      {{
        props.replyTo.state === 'visible'
          ? discussionSnippet(props.replyTo.body)
          : '（这条留言已不可见）'
      }}
    </a>

    <p v-if="deleted" class="text-default-400 mt-1.5 text-sm italic">
      这条留言已删除
    </p>
    <p
      v-else-if="hidden && !props.canModerate"
      class="text-default-400 mt-1.5 text-sm italic"
    >
      这条留言已被审核者隐藏
    </p>
    <template v-else-if="editing">
      <div class="mt-2 space-y-2">
        <KunTextarea
          v-model="editBody"
          aria-label="编辑留言"
          auto-grow
          :rows="2"
          max-height="320px"
          :maxlength="DISCUSSION_MAX_BODY"
          show-char-count
          autofocus
          @keydown="onEditKey"
        />
        <div class="flex justify-end gap-2">
          <KunButton size="sm" variant="light" @click="editing = false">
            取消
          </KunButton>
          <KunButton
            size="sm"
            color="primary"
            :loading="saving"
            :disabled="!editBody.trim()"
            @click="saveEdit"
          >
            保存
          </KunButton>
        </div>
      </div>
    </template>
    <template v-else>
      <div
        class="text-foreground mt-1.5 text-sm leading-relaxed break-words"
        v-html="html"
      />
      <p
        v-if="hidden && props.post.moderation_note"
        class="text-warning-700 mt-1.5 text-xs"
      >
        已隐藏：{{ props.post.moderation_note }}
      </p>
      <p v-else-if="hidden" class="text-warning-700 mt-1.5 text-xs">
        已隐藏，只有讨论管理员能看到原文
      </p>
    </template>

    <div v-if="hiding" class="mt-2 flex flex-wrap items-center gap-2">
      <KunInput
        v-model="hideNote"
        size="sm"
        placeholder="隐藏原因（只有讨论管理员可见，可选）"
        aria-label="隐藏原因"
        class-name="min-w-0 flex-1"
        @keydown.enter.prevent="confirmHide"
      />
      <KunButton size="sm" variant="light" @click="hiding = false">
        取消
      </KunButton>
      <KunButton
        size="sm"
        color="warning"
        :loading="saving"
        @click="confirmHide"
      >
        隐藏
      </KunButton>
    </div>
  </article>
</template>

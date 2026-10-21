<script setup lang="ts">
import type {
  DiscussionParticipant,
  DiscussionPost
} from '#shared/types/discussion'
import type { DiscussionDraft } from '~/composables/useDiscussion'
import {
  DISCUSSION_ANCHOR_LABELS,
  DISCUSSION_OWNER_LABELS
} from '~/constants/discussion'

const props = withDefaults(
  defineProps<{
    anchorKind: string
    anchorId: string
    participants?: DiscussionParticipant[]
  }>(),
  { participants: () => [] }
)

const route = useRoute()
const { session } = useEditorSession()
const thread = useDiscussion(
  () => props.anchorKind,
  () => props.anchorId
)
const composer = ref<{ focus: () => void } | null>(null)
const replyTo = ref<DiscussionPost | null>(null)

const me = computed(() => (session.value ? String(session.value.id) : ''))
const ownerLabel = computed(
  () => DISCUSSION_OWNER_LABELS[props.anchorKind] ?? '发起人'
)
const anchorLabel = computed(
  () => DISCUSSION_ANCHOR_LABELS[props.anchorKind] ?? '这条请求'
)
const byId = computed(
  () => new Map(thread.posts.value.map((post) => [post.id, post]))
)

const candidates = computed(() => {
  const seen = new Set<string>()
  const out: { uid: string; name: string; role?: string }[] = []
  const add = (
    uid: string | null | undefined,
    name?: string,
    role?: string
  ) => {
    if (!uid || !name || uid === me.value || seen.has(uid)) {
      return
    }
    seen.add(uid)
    out.push({ uid, name, role })
  }
  for (const person of props.participants) {
    add(person.uid, person.user?.name, person.role)
  }
  for (const post of thread.posts.value) {
    add(
      post.author_uid,
      post.author?.name,
      post.author_uid === thread.ownerUid.value
        ? ownerLabel.value
        : post.author_reviewer
          ? '审核者'
          : undefined
    )
  }
  return out
})

const watchLabel = computed(() => {
  const viewer = thread.viewer.value
  if (!viewer) {
    return ''
  }
  return viewer.muted ? '已静音' : viewer.watching ? '关注中' : '关注'
})

const toggleWatch = () => {
  const viewer = thread.viewer.value
  if (viewer) {
    thread.setMuted(viewer.watching && !viewer.muted)
  }
}

const startReply = (post: DiscussionPost) => {
  replyTo.value = post
  composer.value?.focus()
}

const submit = async (draft: DiscussionDraft) => {
  replyTo.value = null
  await thread.send(draft)
}

const scrollToHash = () => {
  if (!route.hash.startsWith('#discussion')) {
    return
  }
  nextTick(() =>
    document
      .querySelector(route.hash)
      ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  )
}

onMounted(async () => {
  await thread.load()
  scrollToHash()
})

watch(
  () => [props.anchorKind, props.anchorId],
  () => thread.load()
)
</script>

<template>
  <KunCard
    id="discussion"
    bordered
    padding="md"
    class-name="scroll-mt-20"
    content-class="space-y-3"
  >
    <div class="flex flex-wrap items-center gap-2">
      <h2 class="font-semibold">讨论</h2>
      <KunChip v-if="thread.total.value" size="sm" variant="flat">
        {{ thread.total.value }}
      </KunChip>
      <KunTooltip
        v-if="thread.viewer.value"
        :text="
          thread.viewer.value.muted
            ? '已静音：只有提及你的留言会通知你'
            : thread.viewer.value.watching
              ? '新留言会出现在通知里'
              : '关注后，新留言会出现在通知里'
        "
        position="bottom"
        class-name="ml-auto"
      >
        <KunButton size="sm" variant="light" @click="toggleWatch">
          <KunIcon
            :name="
              thread.viewer.value.watching && !thread.viewer.value.muted
                ? 'lucide:bell-ring'
                : 'lucide:bell-off'
            "
            class="size-4"
          />
          {{ watchLabel }}
        </KunButton>
      </KunTooltip>
    </div>

    <div
      v-if="thread.loading.value && !thread.posts.value.length"
      class="space-y-3"
      aria-busy="true"
      aria-label="正在加载讨论"
    >
      <div v-for="index in 2" :key="index" class="space-y-2 px-3 py-2">
        <div class="flex items-center gap-2">
          <KunSkeleton width="1.5rem" height="1.5rem" rounded="full" />
          <KunSkeleton variant="text" class-name="text-xs" width="6rem" />
        </div>
        <KunSkeleton variant="text" class-name="text-sm" width="85%" />
        <KunSkeleton variant="text" class-name="text-sm" width="60%" />
      </div>
    </div>

    <KunInfo
      v-else-if="thread.loadError.value"
      color="danger"
      variant="flat"
      title="讨论加载失败"
      :description="thread.loadError.value"
    >
      <KunButton size="sm" variant="flat" @click="thread.load()">
        重试
      </KunButton>
    </KunInfo>

    <template v-else>
      <p
        v-if="!thread.posts.value.length && !thread.pending.value.length"
        class="text-default-400 py-2 text-sm"
      >
        还没有人留言。对{{ anchorLabel }}有疑问或补充，可以在这里说明，{{
          ownerLabel
        }}和审核者都会收到通知。
      </p>

      <div v-else class="-mx-3 space-y-1">
        <DiscussionEntry
          v-for="post in thread.posts.value"
          :key="post.id"
          :post="post"
          :reply-to="post.reply_to_id ? byId.get(post.reply_to_id) : null"
          :owner-uid="thread.ownerUid.value"
          :owner-label="ownerLabel"
          :mine="post.author_uid === me"
          :can-moderate="thread.viewer.value?.can_moderate ?? false"
          :can-reply="thread.viewer.value?.can_post ?? false"
          @reply="startReply"
          @edit="(body, done) => thread.edit(post.id, body).then(done)"
          @remove="thread.remove(post.id)"
          @moderate="
            (hidden, note, done) =>
              thread.moderate(post.id, hidden, note).then(done)
          "
        />
        <div
          v-for="item in thread.pending.value"
          :key="item.localId"
          class="px-3 py-2.5"
        >
          <div class="flex items-center gap-2 text-xs">
            <HistoryUser
              :user="
                session
                  ? {
                      object: 'user',
                      id: String(session.id),
                      name: session.name,
                      avatar: session.avatar || null
                    }
                  : null
              "
              size="sm"
            />
            <span v-if="!item.failed" class="text-default-400">发送中…</span>
            <span v-else class="text-danger-600">发送失败</span>
          </div>
          <p
            class="text-default-500 mt-1.5 text-sm break-words whitespace-pre-line"
          >
            {{ item.body }}
          </p>
          <div v-if="item.failed" class="mt-1.5 flex gap-1">
            <KunButton
              size="xs"
              variant="flat"
              color="primary"
              @click="thread.retry(item.localId)"
            >
              重试
            </KunButton>
            <KunButton
              size="xs"
              variant="light"
              @click="thread.discard(item.localId)"
            >
              放弃
            </KunButton>
          </div>
        </div>
      </div>

      <div v-if="thread.nextCursor.value" class="flex justify-center">
        <KunButton
          size="sm"
          variant="light"
          :loading="thread.loadingMore.value"
          @click="thread.loadMore()"
        >
          加载更多留言
        </KunButton>
      </div>

      <template v-if="thread.viewer.value?.can_post">
        <KunDivider />
        <DiscussionComposer
          ref="composer"
          :candidates="candidates"
          :reply-to="replyTo"
          :placeholder="`写下你对${anchorLabel}的问题、补充或回复`"
          @submit="submit"
          @cancel-reply="replyTo = null"
        />
      </template>
    </template>
  </KunCard>
</template>

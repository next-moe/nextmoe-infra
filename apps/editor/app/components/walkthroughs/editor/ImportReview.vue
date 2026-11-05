<script setup lang="ts">
import { cn } from '@kungal/ui-core'
import { importReviewGaps } from '~/utils/walkthrough/importReview'

const emit = defineEmits<{ publish: [] }>()

const doc = useWalkthroughDocument()

const backlog = computed(() => doc.backlog.value)
const progress = computed(() => backlog.value?.progress ?? null)
const gaps = computed(() =>
  progress.value ? importReviewGaps(progress.value) : []
)

const state = computed(() => {
  if (!backlog.value) return null
  if (backlog.value.pendingPublishId && doc.hidden.value) return 'pending'
  if (!doc.hidden.value) return 'publishing'
  return progress.value?.complete ? 'complete' : 'incomplete'
})

const copy = async (text: string) => {
  try {
    await navigator.clipboard.writeText(text)
    useKunMessage(`已复制「${text}」`, 'success')
  } catch {
    useKunMessage('复制失败，请手动选择文字', 'error')
  }
}
</script>

<template>
  <KunInfo
    v-if="state === 'pending' && backlog"
    color="primary"
    variant="flat"
    icon="lucide:hourglass"
    title="上线申请正在审核"
  >
    <p>
      你已经提交了上线这篇攻略的修改，审核通过后它会出现在作品页上，并离开待补全列表。
      <NuxtLink
        :to="`/me/proposals/${backlog.pendingPublishId}`"
        target="_blank"
        class="text-primary-600 hover:underline"
      >
        查看提案 #{{ backlog.pendingPublishId }}
      </NuxtLink>
    </p>
  </KunInfo>

  <KunInfo
    v-else-if="state === 'publishing'"
    color="success"
    variant="flat"
    icon="lucide:rocket"
    title="提交后这篇攻略会上线"
  >
    <template v-if="doc.canHide.value" #title>
      <KunButton
        size="sm"
        variant="light"
        color="default"
        class-name="ml-auto"
        @click="doc.setHidden(true)"
      >
        先不上线
      </KunButton>
    </template>
    <p>这次修改生效后，攻略会出现在作品页上，并离开待补全列表。</p>
  </KunInfo>

  <KunInfo
    v-else-if="state && progress"
    :color="state === 'complete' ? 'success' : 'warning'"
    variant="flat"
    :icon="state === 'complete' ? 'lucide:circle-check' : 'lucide:list-checks'"
    :title="
      state === 'complete'
        ? '导入时缺的内容都补上了'
        : '这篇攻略导入时没有完整提取，补全后提交上线'
    "
  >
    <template v-if="doc.canHide.value" #title>
      <KunButton
        size="sm"
        :color="state === 'complete' ? 'success' : 'warning'"
        :variant="state === 'complete' ? 'solid' : 'light'"
        class-name="ml-auto"
        @click="emit('publish')"
      >
        <KunIcon name="lucide:rocket" class="size-4" />
        {{ state === 'complete' ? '提交上线' : '已经补全了，提交上线' }}
      </KunButton>
    </template>
    <p>
      <template v-if="state === 'complete'">
        确认无误后提交上线，攻略就会出现在作品页上，并离开待补全列表。
      </template>
      <template v-else>{{ gaps.join('；') }}。</template>
      <span class="text-default-500">
        原攻略页标注了 {{ progress.endMarkers }} 个结局，图里连到了
        {{ progress.endingsReached }} 个。
      </span>
      <a
        v-if="backlog?.sourceUrl"
        :href="backlog.sourceUrl"
        target="_blank"
        rel="noopener noreferrer"
        class="text-primary-600 hover:underline"
      >
        对照原攻略
      </a>
    </p>
    <template v-if="progress.items.length">
      <p class="text-default-500 mt-2 text-xs">
        导入时没放进图里的选项，补上后会划掉；点击可以复制：
      </p>
      <div class="mt-1.5 flex flex-wrap gap-1.5">
        <button
          v-for="item in progress.items"
          :key="item.text"
          type="button"
          :class="
            cn(
              'rounded-md px-2 py-0.5 text-xs transition-colors',
              item.added
                ? 'bg-success-100 text-success-700 line-through'
                : 'bg-content1 text-foreground hover:bg-default-100'
            )
          "
          :title="item.added ? '已经在图里了' : '复制这个选项'"
          @click="copy(item.copy)"
        >
          {{ item.text }}
        </button>
      </div>
    </template>
  </KunInfo>
</template>

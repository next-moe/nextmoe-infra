<script setup lang="ts">
import { splitParticipants } from '~/utils/split/timeline'

const { request } = useSplitWorkbench()

const releaseCount = computed(() => request.value?.release_ids.length ?? 0)
</script>

<template>
  <main class="min-h-0 flex-1 overflow-y-auto">
    <div v-if="request" class="mx-auto max-w-4xl space-y-5 px-4 py-6 md:px-8">
      <KunCard bordered padding="md" class-name="space-y-3">
        <div class="grid gap-3 md:grid-cols-[1fr_auto_1fr] md:items-center">
          <NuxtLink
            :to="`/works/${request.target_id}`"
            target="_blank"
            class="border-default-200 hover:border-primary-300 rounded-xl border p-3 transition-colors"
          >
            <p class="text-default-500 flex items-center gap-2 text-xs">
              <span class="bg-success-500 size-2 rounded-full" />
              原作品 #{{ request.target_id }}
            </p>
            <p class="mt-1 font-semibold">{{ request.target_name }}</p>
          </NuxtLink>
          <KunIcon
            name="lucide:arrow-right"
            class="text-default-400 mx-auto size-5 rotate-90 md:rotate-0"
          />
          <component
            :is="request.new_work_id ? 'NuxtLink' : 'div'"
            :to="
              request.new_work_id ? `/works/${request.new_work_id}` : undefined
            "
            target="_blank"
            class="border-default-200 hover:border-primary-300 rounded-xl border p-3 transition-colors"
          >
            <p class="text-default-500 flex items-center gap-2 text-xs">
              <span class="bg-warning-500 size-2 rounded-full" />
              {{
                request.new_work_id
                  ? `新作品 #${request.new_work_id}`
                  : '新作品（未创建）'
              }}
            </p>
            <p class="mt-1 font-semibold">
              {{ request.new_work_name || request.display_name }}
            </p>
          </component>
        </div>
        <p class="text-default-500 text-sm">
          <template v-if="request.state === 'executed'">
            {{ releaseCount }} 个发行已移到新作品。
          </template>
          <template v-else-if="request.state === 'undone'">
            拆分已撤销：{{
              releaseCount
            }}
            个发行已回到原作品，新作品的编号重定向到原作品。
          </template>
          <template v-else-if="request.state === 'rejected'">
            请求已驳回，没有作品被改动。
          </template>
          <template v-else>请求已撤回，没有作品被改动。</template>
        </p>
      </KunCard>

      <SplitWorkbenchRequestPanel />

      <KunCard bordered padding="md">
        <DiscussionThread
          anchor-kind="split_request"
          :anchor-id="request.id"
          :participants="splitParticipants(request)"
        />
      </KunCard>
    </div>
  </main>
</template>

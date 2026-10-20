<script setup lang="ts">
const { application, loaded, load } = useEditorApplication()

onMounted(() => {
  if (!loaded.value) {
    load()
  }
})

const pending = computed(() => application.value?.status === 'pending')
</script>

<template>
  <div
    class="border-primary-200 bg-primary-50 flex flex-col gap-3 rounded-xl border p-4 sm:flex-row sm:items-center"
  >
    <span
      class="bg-primary-100 text-primary flex size-10 shrink-0 items-center justify-center rounded-lg"
    >
      <KunIcon name="lucide:badge-check" class="size-5" />
    </span>
    <div class="min-w-0 flex-1">
      <p class="font-medium">
        {{ pending ? '你的编辑者申请正在审核中' : '经常编辑？申请成为编辑者' }}
      </p>
      <p class="text-default-500 text-sm">
        {{
          pending
            ? '审核通过后，你的修改可以在每日额度内直接生效。'
            : '编辑者的修改无需等待审核即可生效（有每日额度），适合长期维护资料的贡献者。'
        }}
      </p>
    </div>
    <KunButton
      :color="pending ? 'default' : 'primary'"
      :variant="pending ? 'flat' : 'solid'"
      size="sm"
      @click="navigateTo('/apply')"
    >
      {{ pending ? '查看申请' : '去申请' }}
    </KunButton>
  </div>
</template>

<script setup lang="ts">
import { SPLIT_REASON_HINTS } from '~/constants/split'

const open = defineModel<boolean>({ required: true })

const workbench = useSplitWorkbench()
const { preview, selection, draft, busy } = workbench
const router = useRouter()

const note = ref('')
const hints = computed(() => {
  const facts = (preview.value?.facts ?? [])
    .filter((f) => f.code === 'listed_separately')
    .map((f) => f.message.split('；')[0]!)
  return [...facts, ...SPLIT_REASON_HINTS]
})
const addHint = (text: string) => {
  note.value = note.value.trim() ? `${note.value.trim()}\n${text}` : text
}

const submit = async () => {
  const created = await workbench.submit(note.value)
  if (created) {
    open.value = false
    useKunMessage('拆分请求已提交，等待审核', 'success')
    await router.replace(`/split/${created.id}`)
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    title="提交拆分请求"
    description="审核者批准后先进入冷静期，期满后才会新建作品并移走发行；执行后一段时间内可以撤销。"
    size="lg"
  >
    <div class="space-y-4">
      <ul class="bg-default-50 space-y-1 rounded-lg p-3 text-sm">
        <li>
          把 <b>{{ selection.releases.length }}</b> 个发行移到新作品「{{
            draft.displayName
          }}」
        </li>
        <li v-if="selection.promote.length">
          {{ selection.promote.length }} 个资料站条目成为新作品的正式对应
        </li>
        <li>原作品「{{ preview?.target_name }}」保留编号和其余资料</li>
      </ul>
      <div class="space-y-2">
        <p class="text-sm font-medium">为什么它应该是一部单独的作品</p>
        <KunTextarea
          v-model="note"
          placeholder="写给审核者：这个版本和原作有什么不同，哪些资料站单独收录了它"
          aria-label="拆分理由"
          auto-grow
          max-height="240px"
        />
        <div class="flex flex-wrap gap-1.5">
          <KunButton
            v-for="hint in hints"
            :key="hint"
            variant="flat"
            size="xs"
            @click="addHint(hint)"
          >
            <KunIcon name="lucide:plus" class="size-3" />
            {{ hint }}
          </KunButton>
        </div>
      </div>
      <div class="flex justify-end gap-2">
        <KunButton variant="light" @click="open = false">取消</KunButton>
        <KunButton
          color="primary"
          :disabled="note.trim().length < 4"
          :loading="busy === 'submit'"
          @click="submit"
        >
          <KunIcon name="lucide:send" class="size-4" />
          提交
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>

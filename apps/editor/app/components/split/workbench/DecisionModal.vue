<script setup lang="ts">
const props = defineProps<{ kind: 'reject' | 'undo' | null }>()
const emit = defineEmits<{ close: [] }>()

const workbench = useSplitWorkbench()
const { busy } = workbench
const reason = ref('')

const COPY = {
  reject: {
    title: '驳回拆分请求',
    description: '发起人会看到驳回理由。',
    placeholder:
      '例如：VNDB 和 Bangumi 都把它当作同一部作品，请改为在原作品下编辑这个版本。',
    confirm: '驳回',
    done: '已驳回'
  },
  undo: {
    title: '撤销拆分',
    description:
      '发行与资料源对应会原样回到原作品，「已移走」记录与防误配记录一并清除；新作品会并回原作品，它的编号以后重定向到原作品。',
    placeholder: '写明为什么撤销，例如：拆分后发现这是同一游戏的移植版。',
    confirm: '撤销拆分',
    done: '拆分已撤销'
  }
} as const

const open = computed({
  get: () => props.kind !== null,
  set: (value: boolean) => {
    if (!value) {
      emit('close')
    }
  }
})
watch(
  () => props.kind,
  () => (reason.value = '')
)
const copy = computed(() => COPY[props.kind ?? 'reject'])

const confirm = async () => {
  if (!props.kind || !reason.value.trim()) {
    return
  }
  if (await workbench.decide(props.kind, reason.value)) {
    useKunMessage(copy.value.done, 'success')
    emit('close')
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    :title="copy.title"
    :description="copy.description"
    size="md"
  >
    <div class="space-y-4">
      <KunTextarea
        v-model="reason"
        :placeholder="copy.placeholder"
        aria-label="理由"
        auto-grow
        max-height="200px"
      />
      <div class="flex justify-end gap-2">
        <KunButton variant="light" @click="emit('close')">取消</KunButton>
        <KunButton
          color="danger"
          :disabled="!reason.trim()"
          :loading="busy === props.kind"
          @click="confirm"
        >
          {{ copy.confirm }}
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>

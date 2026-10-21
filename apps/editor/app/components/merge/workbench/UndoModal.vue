<script setup lang="ts">
import { ENTITY_META } from '~/constants/editEntities'

const open = defineModel<boolean>({ required: true })

const workbench = useMergeWorkbench()
const { request, family } = workbench
const reason = ref('')

const meta = computed(() => ENTITY_META[family.value])

const confirm = async () => {
  if (!reason.value.trim()) {
    return
  }
  if (await workbench.decide('undo', reason.value)) {
    open.value = false
    reason.value = ''
    useKunMessage('合并已撤销', 'success')
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    title="撤销合并"
    :description="`把 #${request?.source_id} 恢复成独立的${meta.label}，编号不变。`"
    size="lg"
  >
    <div class="space-y-4">
      <div class="grid gap-3 sm:grid-cols-2">
        <div class="bg-success-50 rounded-lg p-3 text-sm">
          <p class="text-success-800 mb-1.5 font-medium">会恢复</p>
          <ul class="text-success-800 space-y-1 pl-4">
            <li class="list-disc">被并入的{{ meta.label }}以原编号重新上线</li>
            <li class="list-disc">
              移过去的发行、标题、资料源对应、评分等移回
            </li>
            <li class="list-disc">合并时隐藏的重复发行重新显示</li>
            <li class="list-disc">保留方被替换的显示名等还原</li>
            <li class="list-disc">重定向撤销，下游站点会收到通知</li>
          </ul>
        </div>
        <div class="bg-warning-50 rounded-lg p-3 text-sm">
          <p class="text-warning-800 mb-1.5 font-medium">不会恢复</p>
          <ul class="text-warning-800 space-y-1 pl-4">
            <li class="list-disc">
              合并时两边合为一条的数据（例如同一位用户在两边的评分）留在保留方
            </li>
            <li class="list-disc">
              合并后新导入或新编辑到保留方的内容不会分回
            </li>
            <li class="list-disc">合并时被关闭的待审提案不会重新打开</li>
          </ul>
        </div>
      </div>
      <KunTextarea
        v-model="reason"
        placeholder="撤销理由（必填）：为什么这次合并是错的"
        aria-label="撤销理由"
        auto-grow
        max-height="200px"
      />
      <div class="flex justify-end gap-2">
        <KunButton variant="light" @click="open = false">取消</KunButton>
        <KunButton
          color="danger"
          :disabled="!reason.trim()"
          :loading="workbench.busy.value === 'undo'"
          @click="confirm"
        >
          撤销合并
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>

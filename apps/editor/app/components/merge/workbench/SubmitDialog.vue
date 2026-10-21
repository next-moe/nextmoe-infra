<script setup lang="ts">
import { ENTITY_META } from '~/constants/editEntities'
import { MERGE_FIELD_LABELS } from '~/constants/merge'
import { choicesBody, duplicateReleases } from '~/utils/merge/plan'

const open = defineModel<boolean>({ required: true })

const workbench = useMergeWorkbench()
const { preview, choices, family, busy } = workbench

const reason = ref('')

const meta = computed(() => ENTITY_META[family.value])
const summary = computed(() => {
  const p = preview.value
  if (!p) {
    return []
  }
  const body = choicesBody(p, choices.value)
  const lines: string[] = []
  const fields = Object.keys(body.field_resolution).map(
    (key) => MERGE_FIELD_LABELS[key] ?? key
  )
  lines.push(
    fields.length
      ? `采用被并入方的${fields.join('、')}`
      : '基本信息以保留方为准，空缺处由被并入方补上'
  )
  if (family.value === 'work') {
    const moving = p.releases.filter((r) => r.side === 'source').length
    lines.push(
      `移入 ${moving} 个发行${body.hide_release_ids.length ? `，其中 ${body.hide_release_ids.length} 个合并后隐藏` : ''}`
    )
    const unhidden = duplicateReleases(p).filter(
      (r) => !body.hide_release_ids.includes(r.id)
    ).length
    if (unhidden) {
      lines.push(`${unhidden} 个重复的发行会保留显示`)
    }
  }
  return lines
})

const addFact = (fact: string) => {
  reason.value = reason.value.trim() ? `${reason.value.trim()}\n${fact}` : fact
}
const canSend = computed(() => reason.value.trim().length >= 4 && !busy.value)

const send = async () => {
  if (!canSend.value) {
    return
  }
  const created = await workbench.submit(reason.value)
  if (created) {
    open.value = false
    reason.value = ''
    useKunMessage('合并请求已提交，等待审核', 'success')
    await navigateTo(`/merge/${created.id}`, { replace: true })
  }
}
</script>

<template>
  <KunModal
    v-model="open"
    title="提交合并请求"
    :description="
      preview
        ? `「${preview.source.name}」并入「${preview.target.name}」，保留后者的编号。`
        : ''
    "
    size="lg"
  >
    <div v-if="preview" class="space-y-4">
      <ul class="bg-default-50 space-y-1 rounded-lg p-3 text-sm">
        <li
          v-for="line in summary"
          :key="line"
          class="text-default-700 flex items-start gap-2"
        >
          <KunIcon
            name="lucide:check"
            class="text-success-600 mt-0.5 size-4 shrink-0"
          />
          {{ line }}
        </li>
        <li
          v-for="(warning, index) in preview.warnings"
          :key="`w${index}`"
          class="text-warning-800 flex items-start gap-2"
        >
          <KunIcon
            name="lucide:triangle-alert"
            class="mt-0.5 size-4 shrink-0"
          />
          {{ warning.message }}
        </li>
      </ul>

      <div class="space-y-2">
        <p class="text-sm font-medium">
          理由
          <span class="text-danger-500">*</span>
        </p>
        <KunTextarea
          v-model="reason"
          :placeholder="`为什么认为它们是同一个${meta.label}？写出依据：同一个外部条目、同一张包装、官方说明……`"
          aria-label="合并理由"
          auto-grow
          max-height="240px"
        />
        <div v-if="preview.facts.length" class="space-y-1">
          <p class="text-default-500 text-xs">
            从数据里找到的依据，点击加入理由：
          </p>
          <div class="flex flex-wrap gap-1.5">
            <button
              v-for="fact in preview.facts"
              :key="fact"
              type="button"
              class="border-default-200 hover:border-primary-300 hover:bg-primary-50 rounded-lg border px-2 py-1 text-left text-xs transition-colors"
              @click="addFact(fact)"
            >
              <KunIcon name="lucide:plus" class="mr-1 inline size-3" />
              {{ fact }}
            </button>
          </div>
        </div>
      </div>

      <p class="text-default-500 text-xs">
        提交后由审核者确认；批准后先经过冷静期（默认 48
        小时）再执行。执行后的一段时间内，审核者可以撤销并恢复被并入的{{
          meta.label
        }}。
      </p>

      <div class="flex justify-end gap-2">
        <KunButton variant="light" @click="open = false">取消</KunButton>
        <KunButton
          color="primary"
          :disabled="!canSend"
          :loading="busy === 'submit'"
          @click="send"
        >
          提交合并请求
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>

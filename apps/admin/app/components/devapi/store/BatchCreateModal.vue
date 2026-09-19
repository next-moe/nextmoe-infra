<script setup lang="ts">
import {
  STORE_COUPON_FACE_PRESETS,
  formatCount,
  parseCouponCodes,
} from '~/constants/store'

const props = defineProps<{ range: [string, string] }>()
const emit = defineEmits<{ created: [id: number] }>()
const open = defineModel<boolean>('open', { required: true })

interface CodeGroup {
  key: number
  faceValue: number | null
  expiresOn: string | null
  codes: string
}

const api = useApi()
let nextKey = 0
const newGroup = (faceValue: number | null): CodeGroup => ({
  key: nextKey++,
  faceValue,
  expiresOn: null,
  codes: '',
})

const name = ref('')
const period = ref<[string | null, string | null]>([null, null])
const note = ref('')
const groups = ref<CodeGroup[]>([])
const error = ref('')
const submitting = ref(false)

watch(open, (v) => {
  if (!v) return
  name.value = ''
  period.value = [...props.range]
  note.value = ''
  groups.value = STORE_COUPON_FACE_PRESETS.map((face) => newGroup(face))
  error.value = ''
})

const parsed = computed(() =>
  groups.value.map((g) => ({ group: g, codes: parseCouponCodes(g.codes) }))
)
const totalCoupons = computed(() =>
  parsed.value.reduce((n, p) => n + p.codes.length, 0)
)
const totalPoints = computed(() =>
  parsed.value.reduce((n, p) => n + (p.group.faceValue ?? 0) * p.codes.length, 0)
)

const removeGroup = (key: number) => {
  groups.value = groups.value.filter((g) => g.key !== key)
}

const handleSubmit = async () => {
  error.value = ''
  const [from, to] = period.value
  if (!name.value.trim()) return (error.value = '请填写批次名称')
  if (!from || !to) return (error.value = '请选择结算区间')
  const coupons = parsed.value.flatMap(({ group, codes }) =>
    codes.map((code) => ({
      face_value: group.faceValue ?? 0,
      code,
      expires_on: group.expiresOn ?? '',
    }))
  )
  if (!coupons.length) return (error.value = '至少录入一张券')

  submitting.value = true
  try {
    const res = await api.post<{ id: number }>('/admin/devapi/store/coupon-batches', {
      name: name.value,
      period_from: from,
      period_to: to,
      note: note.value,
      coupons,
    })
    if (res.code === 0 && res.data) {
      useKunMessage('已存为草稿，确认分配后再发布', 'success')
      emit('created', res.data.id)
    } else {
      error.value = res.message || '录入失败'
    }
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <KunModal v-model="open" size="lg" aria-label="录入一批优惠券">
    <div class="space-y-4">
      <div>
        <h2 class="text-xl font-bold text-foreground">录入一批优惠券</h2>
        <p class="mt-1 text-sm text-default-500">
          先存为草稿：系统按结算区间的去重点击算出建议分配，你核对、调整之后再发布。
        </p>
      </div>

      <KunInput v-model="name" label="批次名称" placeholder="例如 2026-09 第一批" />
      <KunDatePicker
        v-model="period"
        mode="range"
        label="结算区间（JST）"
        class-name="w-full"
      />
      <KunTextarea v-model="note" label="备注（可选）" :rows="2" />

      <div
        v-for="(g, i) in groups"
        :key="g.key"
        class="space-y-3 rounded-lg border border-default-200 p-3"
      >
        <div class="flex items-center justify-between">
          <span class="text-sm font-medium text-foreground">第 {{ i + 1 }} 组</span>
          <KunButton
            v-if="groups.length > 1"
            variant="light"
            color="danger"
            size="sm"
            @click="removeGroup(g.key)"
          >
            移除
          </KunButton>
        </div>
        <div class="grid gap-3 sm:grid-cols-2">
          <KunNumberInput v-model="g.faceValue" label="面额（点）" :min="1" />
          <KunDatePicker v-model="g.expiresOn" label="有效期至（可选）" clearable />
        </div>
        <KunTextarea
          v-model="g.codes"
          label="券码"
          :rows="4"
          placeholder="一行一个，也可以用空格或逗号分隔"
          :hint="`已识别 ${parsed[i]?.codes.length ?? 0} 张`"
        />
      </div>

      <KunButton variant="flat" size="sm" @click="groups.push(newGroup(null))">
        <KunIcon name="lucide:plus" class="mr-1 size-4" />
        添加一组面额
      </KunButton>

      <div class="rounded-lg bg-default-50 p-3 text-sm text-default-500">
        共 {{ totalCoupons }} 张，合计 {{ formatCount(totalPoints) }} 点
      </div>

      <div v-if="error" class="rounded-lg bg-danger-50 p-3 text-sm text-danger">
        {{ error }}
      </div>

      <div class="flex justify-end gap-3">
        <KunButton variant="flat" @click="open = false">取消</KunButton>
        <KunButton color="primary" :disabled="submitting" @click="handleSubmit">
          <KunIcon v-if="submitting" name="lucide:loader-circle" class="mr-2 size-4 animate-spin" />
          存为草稿
        </KunButton>
      </div>
    </div>
  </KunModal>
</template>

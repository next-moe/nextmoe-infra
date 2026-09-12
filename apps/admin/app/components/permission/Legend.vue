<script setup lang="ts">
defineProps<{ managesPermissions: boolean }>()
</script>

<template>
  <KunCard content-class="space-y-3 p-4">
    <div class="flex flex-wrap items-center gap-x-6 gap-y-2 text-sm">
      <span class="inline-flex items-center gap-2">
        <KunIcon name="lucide:check" class="text-success size-4" />
        <span class="text-default-500">代码捆（地板，不可撤销）</span>
      </span>
      <span class="inline-flex items-center gap-2">
        <span class="inline-flex items-center gap-1">
          <KunIcon name="lucide:check" class="text-primary size-4" />
          <span class="bg-primary size-1.5 rounded-full" />
        </span>
        <span class="text-default-500">叠加授权（可撤销）</span>
      </span>
      <span class="inline-flex items-center gap-2">
        <KunChip color="danger" variant="flat" size="xs">
          <span class="line-through">已撤销</span>
        </KunChip>
        <span class="text-default-500">已从代码捆收回（可恢复）</span>
      </span>
      <span class="inline-flex items-center gap-2">
        <span class="text-default-300">—</span>
        <span class="text-default-500">未授予</span>
      </span>
      <span class="inline-flex items-center gap-2">
        <KunIcon name="lucide:lock" class="text-default-400 size-4" />
        <span class="text-default-500">不可委派（只能改代码）</span>
      </span>
    </div>

    <p class="text-default-400 text-sm">
      代码捆是默认值，不是地板：creator / moderator / admin
      三列可以被叠加层收回。ren
      列例外——它是锁死后的恢复保险，任何叠加行都不能削减它。
    </p>

    <p v-if="managesPermissions" class="text-default-400 text-sm">
      你持有 oauth.permissions.manage：可编辑 creator / moderator / admin
      三列（含撤销），但仍受不可委派与包含性（moderator ⊆ admin ⊆ ren）约束。
    </p>
    <p v-else class="text-default-400 text-sm">
      你只能对严格低于自己管理层级的角色操作自己持有的权限——授予与撤销同理；creator
      列仅 ren 可编辑。
    </p>
  </KunCard>
</template>

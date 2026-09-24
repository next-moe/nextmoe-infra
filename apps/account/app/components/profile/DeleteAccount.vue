<script setup lang="ts">
const auth = useAuth()
</script>

<template>
  <KunCard class="border-danger-200 border p-6">
    <h3 class="text-foreground mb-2 text-lg font-semibold">
      <KunIcon name="lucide:user-x" class="text-danger mr-2 inline size-5" />
      注销账号
    </h3>
    <p class="text-default-500 mb-4 text-sm">
      <template v-if="auth.user.value?.deletion_due_at">
        账号已申请注销，将于 {{ formatDeletionDue(auth.user.value.deletion_due_at) }} 执行。
      </template>
      <template v-else>
        永久删除 NextMoe·未萌 账号及其资料。提交后有 7 天冷静期，期间可以撤销。
      </template>
    </p>
    <KunButton href="/delete-account" color="danger" variant="light" size="sm">
      {{ auth.user.value?.deletion_due_at ? '查看或撤销' : '前往注销' }}
    </KunButton>
  </KunCard>
</template>

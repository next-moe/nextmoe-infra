<script setup lang="ts">
import type { DevApp } from '~~/shared/types/dev'

defineProps<{
  app: DevApp
  disabled: boolean
}>()

const emit = defineEmits<{
  edit: []
}>()
</script>

<template>
  <section class="space-y-6">
    <div class="flex items-center justify-between">
      <h2 class="text-foreground text-lg font-semibold">用户登录</h2>
      <KunButton
        :color="app.user_login ? 'default' : 'primary'"
        variant="flat"
        size="sm"
        :disabled="disabled"
        @click="emit('edit')"
      >
        <KunIcon
          :name="app.user_login ? 'lucide:pencil' : 'lucide:log-in'"
          class="mr-1 size-4"
        />
        {{ app.user_login ? '编辑' : '开启用户登录' }}
      </KunButton>
    </div>

    <KunCard content-class="justify-start gap-0" class-name="p-6">
      <div v-if="app.user_login" class="space-y-4">
        <div>
          <p class="text-default-400 text-xs">回调地址</p>
          <div class="mt-1 space-y-1">
            <div
              v-for="uri in app.user_login.redirect_uris"
              :key="uri"
              class="flex min-w-0 items-center gap-2"
            >
              <p class="text-foreground truncate font-mono text-sm">
                {{ uri }}
              </p>
              <KunCopy :text="uri" name="复制" size="sm" />
            </div>
          </div>
        </div>
        <div>
          <p class="text-default-400 text-xs">同意 scope</p>
          <div class="mt-1 flex flex-wrap gap-2">
            <KunChip
              v-for="scope in app.user_login.scopes"
              :key="scope"
              variant="flat"
              size="sm"
            >
              {{ scope }}
            </KunChip>
          </div>
        </div>
        <p class="text-default-500 text-xs">
          PKCE 强制（S256）：授权请求必须携带 code_challenge。
        </p>
      </div>
      <p v-else class="text-default-500 text-sm">
        开启后，应用可通过 OAuth 授权码 + PKCE 流程让 NextMoe
        用户登录；回调地址与 scope 在此自助配置，无需联系平台。
      </p>
    </KunCard>
  </section>
</template>

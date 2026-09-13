<script setup lang="ts">
import { account } from '~/config/account'

useHead({
  htmlAttrs: { lang: 'zh-Hans' },
  titleTemplate: (title) =>
    title ? `${title} - ${account.titleShort}` : account.title
})

useSeoMeta({
  description: account.description,
  themeColor: account.themeColor,
  ogSiteName: account.name,
  ogLocale: 'zh_CN',
  ogType: 'website'
})
</script>

<template>
  <div>
    <NuxtRouteAnnouncer />
    <NuxtLoadingIndicator color="var(--color-primary)" />
    <KunMessageProvider />
    <KunAlertProvider />
    <NuxtLayout>
      <NuxtPage />
    </NuxtLayout>
  </div>
</template>

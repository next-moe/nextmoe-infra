<script setup lang="ts">
import { LANDING_ACCESS_TIERS } from '~/constants/landing'
</script>

<template>
  <div>
    <h2 class="text-foreground text-2xl font-bold tracking-tight">
      先匿名用起来，量大了再拿密钥
    </h2>
    <p class="text-default-500 mt-3 max-w-2xl text-sm leading-relaxed">
      三种身份读到的是同一份数据，区别只在额度怎么算。全部免费，没有付费档位。
    </p>

    <div class="mt-8 grid gap-4 md:grid-cols-3">
      <div
        v-for="tier in LANDING_ACCESS_TIERS"
        :key="tier.name"
        class="border-default-200 bg-content1 flex flex-col rounded-xl border p-5"
      >
        <div class="flex items-center gap-3">
          <div
            class="bg-default-100 text-foreground flex size-10 shrink-0 items-center justify-center rounded-lg"
          >
            <KunIcon :name="tier.icon" class="size-5" />
          </div>
          <div class="min-w-0">
            <h3 class="text-foreground text-base font-semibold">
              {{ tier.name }}
            </h3>
            <p class="text-default-400 truncate font-mono text-xs">
              {{ tier.credential }}
            </p>
          </div>
        </div>
        <p
          class="text-foreground mt-4 text-lg font-semibold tracking-tight tabular-nums"
        >
          {{ tier.limit }}
        </p>
        <p class="text-default-400 text-xs">{{ tier.counted }}</p>
        <p class="text-default-500 mt-3 flex-1 text-sm leading-relaxed">
          {{ tier.body }}
        </p>
        <NuxtLink
          :to="tier.link.to"
          class="text-primary mt-4 inline-flex items-center gap-1 text-sm font-medium hover:underline"
        >
          {{ tier.link.label }}
          <KunIcon name="lucide:arrow-right" class="size-4" />
        </NuxtLink>
      </div>
    </div>

    <p class="text-default-400 mt-6 text-xs leading-relaxed">
      匿名与用户令牌的数字是默认值，以响应头
      <code class="text-default-500 font-mono">RateLimit-Policy</code>
      为准。分档细节见
      <NuxtLink to="/docs/rate-limits" class="text-primary hover:underline">
        限流与配额
      </NuxtLink>
      ，选哪种凭据见
      <NuxtLink to="/docs/authentication" class="text-primary hover:underline">
        鉴权与凭据
      </NuxtLink>
      。
    </p>
  </div>
</template>

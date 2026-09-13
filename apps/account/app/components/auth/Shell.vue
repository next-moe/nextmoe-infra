<script setup lang="ts">
import { AUTH_ART, type AuthArt } from '~/constants/auth-art'

withDefaults(defineProps<{ art?: AuthArt }>(), { art: () => AUTH_ART.login })
</script>

<template>
  <div class="bg-background grid min-h-svh lg:grid-cols-2">
    <div class="flex min-w-0 flex-col px-6 py-7 sm:px-10 lg:px-14 lg:py-9">
      <header class="flex shrink-0 items-center justify-between gap-4">
        <NuxtLink to="/" class="flex min-w-0 items-center gap-3">
          <img
            src="/favicon.webp"
            alt=""
            width="40"
            height="40"
            class="border-default-200 size-10 shrink-0 rounded-xl border object-cover"
          >
          <span class="flex min-w-0 flex-col leading-tight">
            <span class="text-foreground truncate text-sm font-semibold">
              NextMoe·未萌
            </span>
            <span class="text-default-400 truncate text-xs">账号中心</span>
          </span>
        </NuxtLink>

        <LayoutColorModeToggle />
      </header>

      <div
        class="bg-brand-ink border-brand-line relative mt-6 h-32 shrink-0 overflow-hidden rounded-2xl border lg:hidden"
      >
        <img
          :src="art.src"
          alt=""
          class="pointer-events-none absolute -top-1 right-3 h-44 w-auto max-w-none object-contain object-top select-none"
        >
        <div class="relative max-w-[58%] p-5">
          <p class="text-[0.65rem] tracking-[0.28em] text-white/45 uppercase">
            NextMoe
          </p>
          <p class="mt-2 text-lg leading-snug font-semibold text-balance text-white">
            {{ art.headline }}
          </p>
        </div>
      </div>

      <main class="flex flex-1 items-center py-10 lg:py-14">
        <div class="mx-auto w-full max-w-[26rem]">
          <slot />
        </div>
      </main>

      <footer
        class="text-default-400 flex shrink-0 flex-wrap items-center gap-x-5 gap-y-2 text-xs"
      >
        <span>© NextMoe·未萌</span>
        <a
          href="https://www.nextmoe.com/terms"
          target="_blank"
          rel="noopener"
          class="hover:text-default-600 transition-colors"
        >服务条款</a>
        <a
          href="https://www.nextmoe.com/privacy"
          target="_blank"
          rel="noopener"
          class="hover:text-default-600 transition-colors"
        >隐私政策</a>
      </footer>
    </div>

    <aside
      class="bg-brand-ink border-brand-line relative hidden overflow-hidden border-l lg:block"
    >
      <span
        class="border-brand-line absolute -top-32 -right-32 size-80 rounded-full border"
        aria-hidden="true"
      />
      <span
        class="border-brand-line absolute top-20 -right-56 size-[34rem] rounded-full border"
        aria-hidden="true"
      />
      <img
        :src="art.src"
        alt=""
        class="pointer-events-none absolute right-0 bottom-0 h-[78%] max-h-[46rem] w-auto max-w-none translate-x-[3%] object-contain object-bottom select-none"
      >

      <div class="relative p-12 xl:p-16">
        <p class="text-xs font-medium tracking-[0.32em] text-white/45 uppercase">
          NextMoe
        </p>
        <h2
          class="mt-7 max-w-[13ch] text-3xl leading-[1.35] font-semibold tracking-tight text-balance text-white xl:text-4xl"
        >
          {{ art.headline }}
        </h2>
        <p
          v-if="art.caption"
          class="mt-5 max-w-[19rem] text-sm leading-relaxed text-pretty text-white/55"
        >
          {{ art.caption }}
        </p>
      </div>
    </aside>
  </div>
</template>

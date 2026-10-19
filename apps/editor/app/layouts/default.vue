<script setup lang="ts">
import { cn } from '@kungal/ui-core'

const SIDEBAR_KEY = 'nextmoe-editor-sidebar-collapsed'

const { load } = useEditorSession()
const { capture } = useReturnLink()
const route = useRoute()

const collapsed = ref(false)
const mobileNavOpen = ref(false)

const toggleCollapsed = () => {
  collapsed.value = !collapsed.value
  try {
    localStorage.setItem(SIDEBAR_KEY, collapsed.value ? '1' : '0')
  } catch {
    // Collapse state is a convenience; losing it is harmless.
  }
}

onMounted(() => {
  try {
    collapsed.value = localStorage.getItem(SIDEBAR_KEY) === '1'
  } catch {
    collapsed.value = false
  }
})

watch(
  () => route.fullPath,
  () => {
    mobileNavOpen.value = false
  }
)

capture()
await callOnce('editor-session', load)
</script>

<template>
  <div class="bg-background text-foreground flex min-h-dvh flex-col">
    <LayoutTopbar @open-nav="mobileNavOpen = true" />

    <div class="flex min-h-0 flex-1">
      <aside
        :class="
          cn(
            'border-default-200 bg-content1 sticky top-12 hidden h-[calc(100dvh-3rem)] shrink-0 flex-col border-r transition-[width] duration-200 md:flex',
            collapsed ? 'w-14' : 'w-60'
          )
        "
      >
        <LayoutSidebarNav :collapsed="collapsed" class="flex-1" />
        <button
          type="button"
          :aria-label="collapsed ? '展开侧边栏' : '折叠侧边栏'"
          class="border-default-200 text-default-500 hover:bg-default-100 hover:text-foreground flex h-10 items-center justify-center border-t transition-colors"
          @click="toggleCollapsed"
        >
          <KunIcon
            :name="
              collapsed ? 'lucide:panel-left-open' : 'lucide:panel-left-close'
            "
            class="size-4"
          />
        </button>
      </aside>

      <main class="min-w-0 flex-1 px-4 py-5 md:px-8 md:py-6">
        <slot />
      </main>
    </div>

    <KunDrawer
      v-model="mobileNavOpen"
      placement="left"
      :responsive="false"
      size="sm"
      aria-label="导航"
      inner-class-name="p-0"
    >
      <LayoutSidebarNav />
    </KunDrawer>
  </div>
</template>

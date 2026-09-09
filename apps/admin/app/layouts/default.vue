<script setup lang="ts">
import { SIDEBAR_MENU, type SidebarItem } from '~/constants/admin'
import { useBodyScrollLock } from '@kungal/ui-vue'

const auth = useAuth()
const route = useRoute()
const router = useRouter()
const colorMode = useColorMode()

const isSidebarCollapsed = ref(false)
const isMobileMenuOpen = ref(false)

const canSee = (item: SidebarItem) =>
  (!item.adminOnly || auth.isAdmin.value) && (!item.renOnly || auth.isRen.value)

const visibleMenu = computed(() =>
  SIDEBAR_MENU.filter(canSee).map((item) => ({
    ...item,
    children: item.children?.filter(canSee)
  }))
)

const expandedGroups = ref<Record<string, boolean>>({})
const isGroupOpen = (item: SidebarItem) =>
  item.label in expandedGroups.value
    ? expandedGroups.value[item.label]
    : (item.children?.some((c) => c.to === route.path) ?? false)
const toggleGroup = (item: SidebarItem) => {
  expandedGroups.value = {
    ...expandedGroups.value,
    [item.label]: !isGroupOpen(item)
  }
}

const colorModeOptions = [
  { value: 'light', label: '浅色', icon: 'lucide:sun' },
  { value: 'dark', label: '深色', icon: 'lucide:moon' },
  { value: 'system', label: '跟随系统', icon: 'lucide:monitor' },
] as const

const setColorMode = (mode: string) => {
  colorMode.preference = mode
}

// would otherwise leave it open over the destination page.
watch(
  () => route.path,
  () => {
    isMobileMenuOpen.value = false
  }
)

const { lock, unlock } = useBodyScrollLock()
let locked = false
watch(isMobileMenuOpen, (open) => {
  if (open && !locked) {
    lock()
    locked = true
  } else if (!open && locked) {
    unlock()
    locked = false
  }
})
onUnmounted(() => {
  if (locked) {
    unlock()
    locked = false
  }
})

onMounted(() => {
  if (!auth.user.value) {
    router.push('/auth/login')
  }
})

await callOnce('auth:user', async () => {
  if (!auth.user.value) {
    await auth.fetchUser()
  }
})
</script>

<template>
  <div class="bg-background flex min-h-screen">
    <Transition
      enter-active-class="transition-opacity duration-300 ease-out"
      enter-from-class="opacity-0"
      leave-active-class="transition-opacity duration-200 ease-in"
      leave-to-class="opacity-0"
    >
      <button
        v-if="isMobileMenuOpen"
        type="button"
        aria-label="关闭菜单"
        class="fixed inset-0 z-40 bg-black/50 backdrop-blur-sm md:hidden"
        @click="isMobileMenuOpen = false"
      />
    </Transition>

    <aside
      :class="[
        'bg-content1 fixed inset-y-0 left-0 z-50 flex w-64 flex-col shadow-lg transition-all duration-300',
        isMobileMenuOpen ? 'translate-x-0' : '-translate-x-full',
        'md:translate-x-0',
        isSidebarCollapsed ? 'md:w-16' : 'md:w-64'
      ]"
    >
      <div
        class="border-default-200 flex h-16 items-center justify-between gap-2 border-b px-4"
      >
        <NuxtLink to="/" class="flex min-w-0 items-center gap-2">
          <img
            src="/favicon.webp"
            alt="鲲 Galgame OAuth"
            class="size-8 shrink-0 rounded-lg"
          />
          <span
            class="text-primary truncate text-lg font-bold"
            :class="isSidebarCollapsed && 'md:hidden'"
          >
            鲲 Galgame OAuth
          </span>
        </NuxtLink>
        <KunButton
          variant="light"
          size="sm"
          is-icon-only
          aria-label="关闭菜单"
          class-name="md:hidden shrink-0"
          @click="isMobileMenuOpen = false"
        >
          <KunIcon name="lucide:x" class="size-5" />
        </KunButton>
      </div>

      <nav class="flex-1 space-y-1 overflow-y-auto p-4">
        <template v-for="item in visibleMenu" :key="item.label">
          <NuxtLink
            v-if="!item.children"
            :to="item.to"
            class="text-default-600 hover:bg-primary-50 hover:text-primary flex items-center gap-3 rounded-lg px-3 py-2 transition-colors"
            active-class="bg-primary-50 text-primary"
          >
            <KunIcon :name="item.icon" class="size-5 shrink-0" />
            <span v-if="!isSidebarCollapsed" class="truncate">{{ item.label }}</span>
          </NuxtLink>

          <template v-else-if="isSidebarCollapsed">
            <NuxtLink
              v-for="child in item.children"
              :key="child.to"
              :to="child.to"
              class="text-default-600 hover:bg-primary-50 hover:text-primary flex items-center gap-3 rounded-lg px-3 py-2 transition-colors"
              active-class="bg-primary-50 text-primary"
            >
              <KunIcon :name="child.icon" class="size-5 shrink-0" />
            </NuxtLink>
          </template>

          <div v-else>
            <button
              type="button"
              class="text-default-600 hover:bg-primary-50 hover:text-primary flex w-full items-center gap-3 rounded-lg px-3 py-2 transition-colors"
              @click="toggleGroup(item)"
            >
              <KunIcon :name="item.icon" class="size-5 shrink-0" />
              <span class="flex-1 truncate text-left">{{ item.label }}</span>
              <KunIcon
                :name="isGroupOpen(item) ? 'lucide:chevron-down' : 'lucide:chevron-right'"
                class="size-4 shrink-0"
              />
            </button>
            <div v-if="isGroupOpen(item)" class="mt-1 space-y-1 pl-4">
              <NuxtLink
                v-for="child in item.children"
                :key="child.to"
                :to="child.to"
                class="text-default-600 hover:bg-primary-50 hover:text-primary flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors"
                active-class="bg-primary-50 text-primary"
              >
                <KunIcon :name="child.icon" class="size-4 shrink-0" />
                <span class="truncate">{{ child.label }}</span>
              </NuxtLink>
            </div>
          </div>
        </template>
      </nav>

      <KunButton
        variant="light"
        rounded="none"
        is-icon-only
        aria-label="折叠侧边栏"
        class-name="border-default-200 hidden h-12 border-t md:flex"
        @click="isSidebarCollapsed = !isSidebarCollapsed"
      >
        <KunIcon
          :name="
            isSidebarCollapsed ? 'lucide:chevron-right' : 'lucide:chevron-left'
          "
          class="size-5"
        />
      </KunButton>
    </aside>

    <div
      :class="[
        'flex min-w-0 flex-1 flex-col transition-all duration-300',
        isSidebarCollapsed ? 'md:ml-16' : 'md:ml-64'
      ]"
    >
      <header
        class="border-default-200 bg-content1 sticky top-0 z-30 flex h-16 items-center justify-between gap-2 border-b px-4 shadow-sm md:px-6"
      >
        <div class="flex min-w-0 items-center gap-2 md:gap-4">
          <KunButton
            variant="light"
            size="sm"
            is-icon-only
            aria-label="打开菜单"
            class-name="md:hidden shrink-0"
            @click="isMobileMenuOpen = true"
          >
            <KunIcon name="lucide:menu" class="size-5" />
          </KunButton>
          <h2 class="text-foreground truncate text-lg font-semibold">管理后台</h2>
        </div>

        <div class="flex shrink-0 items-center gap-2 md:gap-4">
          <KunPopover position="bottom-end">
            <template #trigger>
              <KunButton
                variant="light"
                size="md"
                is-icon-only
                aria-label="切换主题"
              >
                <KunIcon name="lucide:sun-moon" class="size-6" />
              </KunButton>
            </template>

            <div class="w-36 py-1">
              <button
                v-for="option in colorModeOptions"
                :key="option.value"
                class="flex w-full items-center gap-3 px-3 py-2 text-sm transition-colors"
                :class="
                  colorMode.preference === option.value
                    ? 'bg-primary-50 text-primary'
                    : 'text-default-500 hover:bg-default-100 hover:text-foreground'
                "
                @click="setColorMode(option.value)"
              >
                <KunIcon :name="option.icon" class="size-4" />
                <span>{{ option.label }}</span>
              </button>
            </div>
          </KunPopover>

          <LayoutAccountSwitcher />
        </div>
      </header>

      <main class="flex-1 p-4 md:p-6">
        <slot />
      </main>
    </div>
  </div>
</template>

export const useUserStore = defineStore('user', () => {
  const user = ref<User | null>(null)

  const isLoggedIn = computed(() => !!user.value)
  const isAdmin = computed(() => user.value?.roles?.includes('admin') ?? false)
  const isRen = computed(() => user.value?.roles?.includes('ren') ?? false)

  const setUser = (u: User) => {
    user.value = u
  }

  const clearUser = () => {
    user.value = null
  }

  return {
    user,
    isLoggedIn,
    isAdmin,
    isRen,
    setUser,
    clearUser,
  }
}, {
  persist: {
    pick: ['user'],
  },
})

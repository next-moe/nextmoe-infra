export const useLoginModal = () => {
  const isOpen = useState<boolean>('login-modal-open', () => false)
  const redirect = useState<string | null>('login-modal-redirect', () => null)

  const open = (to?: string | null) => {
    redirect.value = isSafeInternalPath(to) ? to : null
    isOpen.value = true
  }

  const close = () => {
    isOpen.value = false
  }

  return { isOpen, redirect, open, close }
}

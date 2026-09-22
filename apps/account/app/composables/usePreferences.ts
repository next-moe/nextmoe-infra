import type {
  AdultConfirmationResponse,
  NsfwDisplay,
  NsfwDisplayResponse,
  PreferenceSummary
} from '~~/shared/types/preferences'

export const usePreferences = () => {
  const api = useApi()
  const userStore = useUserStore()

  const patchUser = (patch: Partial<User>) => {
    if (userStore.user) userStore.setUser({ ...userStore.user, ...patch })
  }

  const confirmAdult = async () => {
    const response = await api.post<AdultConfirmationResponse>(
      '/auth/me/adult-confirmation'
    )
    if (response.code === 0) {
      patchUser({ adult_confirmed_at: response.data.adult_confirmed_at })
    }
    return response
  }

  const setNsfwDisplay = async (nsfwDisplay: NsfwDisplay) => {
    const response = await api.put<NsfwDisplayResponse>('/auth/me/nsfw', {
      nsfw_display: nsfwDisplay
    })
    if (response.code === 0) {
      patchUser({
        nsfw_display: response.data.nsfw_display,
        adult_confirmed_at: response.data.adult_confirmed_at
      })
    }
    return response
  }

  const listPreferences = () =>
    api.get<PreferenceSummary[]>('/auth/me/preferences')

  const deletePreference = (namespace: string) =>
    api.delete(`/auth/me/preferences/${encodeURIComponent(namespace)}`)

  return { confirmAdult, setNsfwDisplay, listPreferences, deletePreference }
}

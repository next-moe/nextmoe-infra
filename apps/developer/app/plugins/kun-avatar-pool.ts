import { installKunUIConfig } from '@kungal/ui-vue'
import { AVATAR_POOL_FALLBACK, fetchAvatarPool } from '~/utils/avatarPool'

// Hands KunUI the images it picks a default avatar from. Without this the pool
// is `[]` and every avatar-less account on the site renders one identical
// picture -- see `utils/avatarPool.ts` for the measurement.
//
// `useState` is what keeps server and client picking the SAME image: the list
// rides the Nuxt payload, so hydration sees the same array -- and therefore the
// same `hash(name) % pool.length` -- as the server did. Refetching in the
// browser could return a pool of a different length and reshuffle every avatar
// on hydration.
//
// `/dashboard/**` is `ssr: false` (nuxt.config routeRules), so on those routes
// there is no payload and no server pass: they run on the baked snapshot in
// AVATAR_POOL_FALLBACK. That is a full 64-entry pool, so avatars still vary --
// they just do not pick up a re-curation until the next deploy.
//
// `installKunUIConfig` merges over whatever a previous call installed, so this
// plugin and the @kungal/ui-nuxt layer's own config plugin may run in either
// order.
export default defineNuxtPlugin(async (nuxtApp) => {
  const pool = useState<string[]>('kun-avatar-pool', () => AVATAR_POOL_FALLBACK)
  if (import.meta.server) pool.value = await fetchAvatarPool()
  installKunUIConfig(nuxtApp.vueApp, { avatarFallbackPool: pool.value })
})

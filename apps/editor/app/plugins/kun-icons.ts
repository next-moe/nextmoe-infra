import { registerKunIcons } from '@kungal/ui-core'
import { KUN_ICONS } from '~/assets/kun-icons'

export default defineNuxtPlugin(() => {
  registerKunIcons(KUN_ICONS)
})

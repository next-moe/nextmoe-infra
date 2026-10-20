import { setImageCdnBase } from '~/utils/imageCdn'

export default defineNuxtPlugin(() => {
  setImageCdnBase(useRuntimeConfig().public.imageCdnBase as string)
})

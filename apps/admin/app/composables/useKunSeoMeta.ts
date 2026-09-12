import { admin } from '~/config/admin'
import type {
  ActiveHeadEntry,
  UseHeadOptions,
  UseSeoMetaInput
} from '@unhead/vue'
import type { NuxtApp } from '#app/nuxt'

interface NuxtUseHeadOptions extends UseHeadOptions {
  nuxt?: NuxtApp
}

/**
 * title, description, ogType?, ogImage? required
 */
export const useKunSeoMeta = (
  input: Omit<
    UseSeoMetaInput,
    | 'ogUrl'
    | 'ogTitle'
    | 'ogDescription'
    | 'twitterCard'
    | 'twitterTitle'
    | 'twitterDescription'
    | 'twitterImage'
    | 'twitterImageAlt'
  >,
  options?: NuxtUseHeadOptions
  // eslint-disable-next-line @typescript-eslint/no-invalid-void-type
): ActiveHeadEntry<UseSeoMetaInput> | void => {
  const pageTitle = input.title?.toString() ?? ''
  const fullTitle = pageTitle
    ? `${pageTitle} - ${admin.title}`
    : admin.title
  const description = input.description?.toString() ?? admin.description
  const route = useRoute()

  const pageUrl = `${admin.domain.main}${route.path}`
  const image = input.ogImage
    ? input.ogImage
    : admin.images[0]
      ? admin.images[0].fullUrl
      : '/favicon.webp'

  useSeoMeta(
    {
      title: pageTitle,
      description,
      keywords: admin.keywords.toString(),
      ogUrl: pageUrl,
      ogType: input.ogType || 'website',
      ogTitle: fullTitle,
      ogDescription: description,
      ogImage: image,
      ogImageAlt: fullTitle,
      twitterCard: 'summary_large_image',
      twitterTitle: fullTitle,
      twitterDescription: description,
      twitterImage: image,
      twitterImageAlt: fullTitle,
      ...input
    },
    options
  )


  useHead({
    link: [{ rel: 'canonical', href: pageUrl }]
  })
}

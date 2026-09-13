import { account } from '~/config/account'
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
    ? `${pageTitle} - ${account.title}`
    : account.title
  const description = input.description?.toString() ?? account.description
  const route = useRoute()

  const pageUrl = `${account.domain.main}${route.path}`
  const image = input.ogImage
    ? input.ogImage
    : account.images[0]
      ? account.images[0].fullUrl
      : '/favicon.webp'

  useSeoMeta(
    {
      title: pageTitle,
      description,
      keywords: account.keywords.toString(),
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

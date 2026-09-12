import tailwindcss from '@tailwindcss/vite'

export default defineNuxtConfig({
  compatibilityDate: '2025-07-15',

  devtools: { enabled: false },

  extends: ['@kungal/ui-nuxt'],

  app: {
    head: {
      htmlAttrs: { lang: 'zh-Hans' },
      meta: [
        { name: 'robots', content: 'noindex, nofollow' },
        {
          name: 'theme-color',
          media: '(prefers-color-scheme: light)',
          content: '#f4f4f7'
        },
        {
          name: 'theme-color',
          media: '(prefers-color-scheme: dark)',
          content: '#0a0a0a'
        }
      ],
      link: [
        { rel: 'icon', type: 'image/x-icon', href: '/favicon.ico' },
        {
          rel: 'icon',
          type: 'image/png',
          sizes: '32x32',
          href: '/favicon-32x32.png'
        },
        {
          rel: 'icon',
          type: 'image/png',
          sizes: '16x16',
          href: '/favicon-16x16.png'
        },
        {
          rel: 'apple-touch-icon',
          sizes: '180x180',
          href: '/apple-touch-icon.png'
        }
      ]
    }
  },

  css: ['~/assets/css/main.css'],

  routeRules: {
    '/**': { headers: { 'X-Robots-Tag': 'noindex, nofollow' } }
  },

  modules: [
    '@nuxt/eslint',
    '@nuxtjs/color-mode',
    '@pinia/nuxt',
    'pinia-plugin-persistedstate/nuxt',
    'nuxt-schema-org',
    'nuxt-echarts'
  ],

  echarts: {
    renderer: 'canvas',
    charts: ['BarChart'],
    components: ['GridComponent', 'TooltipComponent', 'MarkLineComponent']
  },

  devServer: {
    host: '127.0.0.1',
    port: 9421
  },

  pinia: {
    storesDirs: ['./store/**']
  },

  piniaPluginPersistedstate: {
    cookieOptions: {
      maxAge: 60 * 60 * 24 * 7,
      sameSite: 'strict'
    }
  },

  colorMode: {
    preference: 'system',
    fallback: 'light',
    globalName: '__NEXTMOE_ADMIN_COLOR_MODE__',
    componentName: 'ColorScheme',
    classPrefix: 'kun-',
    classSuffix: '-mode',
    storageKey: 'nextmoe-admin-color-mode'
  },

  vite: {
    // @ts-expect-error ts-expect-error
    plugins: [tailwindcss()]
  },

  runtimeConfig: {
    apiBaseSsr: process.env.NUXT_API_BASE_SSR || '',
    catalogApiBaseSsr: process.env.NUXT_CATALOG_API_BASE_SSR || '',
    trustApiBaseSsr: process.env.NUXT_TRUST_API_BASE_SSR || '',
    aiApiBaseSsr: process.env.NUXT_AI_API_BASE_SSR || '',
    oauthClientSecret: process.env.NUXT_OAUTH_CLIENT_SECRET || '',
    public: {
      apiBase:
        process.env.KUN_VISUAL_NOVEL_NUXT_PUBLIC_API_BASE ||
        'http://127.0.0.1:9277/api/v1',
      catalogApiBase:
        process.env.KUN_VISUAL_NOVEL_NUXT_PUBLIC_CATALOG_API_BASE ||
        '/catalog-proxy',
      trustApiBase:
        process.env.KUN_VISUAL_NOVEL_NUXT_PUBLIC_TRUST_API_BASE ||
        '/trust-proxy',
      aiApiBase:
        process.env.KUN_VISUAL_NOVEL_NUXT_PUBLIC_AI_API_BASE || '/ai-proxy',
      imageCdnBase:
        process.env.KUN_VISUAL_NOVEL_NUXT_PUBLIC_IMAGE_CDN_BASE ||
        'https://image.kungal.iloveren.link',
      oauthAuthorizeBase:
        process.env.NUXT_PUBLIC_OAUTH_AUTHORIZE_BASE ||
        'https://account.nextmoe.com/api/v1',
      oauthWebBase:
        process.env.NUXT_PUBLIC_OAUTH_WEB_BASE || 'https://account.nextmoe.com',
      oauthClientId: process.env.NUXT_PUBLIC_OAUTH_CLIENT_ID || 'nextmoe-admin',
      oauthRedirectUri:
        process.env.NUXT_PUBLIC_OAUTH_REDIRECT_URI ||
        'https://admin.nextmoe.dev/auth/callback'
    }
  }
})

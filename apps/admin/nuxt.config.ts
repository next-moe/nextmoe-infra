import tailwindcss from '@tailwindcss/vite'

export default defineNuxtConfig({
  compatibilityDate: '2025-07-15',

  devtools: { enabled: false },

  extends: ['@kungal/ui-nuxt'],

  css: ['~/assets/css/main.css'],

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
    port: 9420
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
    globalName: '__KUNGALGAME_COLOR_MODE__',
    componentName: 'ColorScheme',
    classPrefix: 'kun-',
    classSuffix: '-mode',
    storageKey: 'kungalgame-color-mode'
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
        'https://image.kungal.iloveren.link'
    }
  }
})

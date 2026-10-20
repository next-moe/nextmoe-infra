import tailwindcss from '@tailwindcss/vite'

export default defineNuxtConfig({
  compatibilityDate: '2025-07-15',

  ssr: false,

  devtools: { enabled: false },

  extends: ['@kungal/ui-nuxt'],

  app: {
    head: {
      title: '未萌编辑台',
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
        },
        { rel: 'manifest', href: '/site.webmanifest' }
      ]
    }
  },

  css: ['~/assets/css/main.css'],

  routeRules: {
    '/**': { headers: { 'X-Robots-Tag': 'noindex, nofollow' } }
  },

  modules: ['@nuxt/eslint', '@nuxtjs/color-mode'],

  devServer: {
    host: '127.0.0.1',
    port: 9422
  },

  colorMode: {
    preference: 'system',
    fallback: 'light',
    globalName: '__NEXTMOE_EDITOR_COLOR_MODE__',
    componentName: 'ColorScheme',
    classPrefix: 'kun-',
    classSuffix: '-mode',
    storageKey: 'nextmoe-editor-color-mode'
  },

  vite: {
    plugins: [tailwindcss()]
  },

  runtimeConfig: {
    oauthApiBaseSsr: '',
    catalogApiBaseSsr: 'http://127.0.0.1:9281',
    public: {
      oauthAuthorizeBase: 'http://127.0.0.1:9277/api/v1',
      oauthWebBase: 'http://127.0.0.1:9420',
      oauthClientId: 'nextmoe-editor',
      oauthRedirectUri: 'http://127.0.0.1:9422/auth/callback',
      imageCdnBase: 'https://image.kungal.iloveren.link'
    }
  }
})

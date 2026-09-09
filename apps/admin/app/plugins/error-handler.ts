export default defineNuxtPlugin((nuxtApp) => {
  nuxtApp.vueApp.config.errorHandler = (error, instance, info) => {
    console.error('Vue Error:', error)
    console.error('Component:', instance)
    console.error('Info:', info)
  }

  nuxtApp.hook('vue:error', (error, instance, info) => {
    console.error('Vue Error Hook:', error)
  })

  nuxtApp.hook('app:error', (error) => {
    console.error('App Error:', error)
  })
})

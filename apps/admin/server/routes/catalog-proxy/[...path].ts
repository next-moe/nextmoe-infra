export default defineEventHandler((event) => {
  const path = event.context.params?.path ?? ''
  const segments = path.split('/')
  if (segments[0] !== 'admin' || segments.some((s) => s === '' || s === '..')) {
    throw createError({ statusCode: 404, statusMessage: 'Not Found' })
  }
  const config = useRuntimeConfig()
  const base =
    (config.catalogApiBaseSsr as string) || 'http://127.0.0.1:9281/api/v1'
  const search = getRequestURL(event).search
  return proxyRequest(event, `${base}/${path}${search}`)
})

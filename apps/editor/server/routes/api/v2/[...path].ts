export default defineEventHandler(async (event) => {
  const v2Path = event.path.slice('/api'.length)
  if (!isAllowedV2Path(v2Path)) {
    throw createError({ statusCode: 404, statusMessage: 'Not Found' })
  }
  return proxyUpstream(event, editorServerConfig(event).catalogBase, v2Path, 'Catalog')
})

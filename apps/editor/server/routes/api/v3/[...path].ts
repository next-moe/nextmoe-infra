export default defineEventHandler(async (event) => {
  const v3Path = event.path.slice('/api'.length)
  if (!isAllowedV3Path(v3Path)) {
    throw createError({ statusCode: 404, statusMessage: 'Not Found' })
  }
  return proxyUpstream(event, editorServerConfig(event).canonBase, v3Path, 'Canon')
})

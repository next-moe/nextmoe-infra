export default defineEventHandler(async (event) => {
  const navigation = isPageNavigation({
    method: event.method,
    path: event.path,
    accept: getRequestHeader(event, 'accept'),
    fetchDest: getRequestHeader(event, 'sec-fetch-dest')
  })
  if (!navigation) {
    return
  }
  rememberOrigin(event)
  if (await ensureEditorSession(event)) {
    return
  }
  return startSilentLogin(event)
})

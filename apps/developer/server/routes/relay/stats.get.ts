// Every landing render fetches this, and to the API this server is a single
// anonymous caller: one IP bucket of 30 calls a minute and 1,000 a day.
export default defineCachedEventHandler(
  (event) =>
    $fetch(`${useRuntimeConfig(event).public.nextmoeApiBase}/v2/catalog/stats`),
  { maxAge: 600, swr: true }
)

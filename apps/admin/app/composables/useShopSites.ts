export const useShopSites = () => {
  const sites = useState<ShopSite[]>('shop-sites', () => [])
  const api = useApi()

  const load = async () => {
    if (sites.value.length) return
    const res = await api.get<ShopSite[]>('/admin/shop/sites')
    if (res.code === 0) sites.value = res.data ?? []
  }

  const nameOf = (id: number | null) =>
    id ? (sites.value.find((s) => s.id === id)?.name ?? `站点 #${id}`) : '全站'

  const options = computed(() => [
    { value: '' as const, label: '全站（所有站点都卖）' },
    ...sites.value.map((s) => ({
      value: s.id,
      label: `${s.name}（${s.domain}）`
    }))
  ])

  return { sites, load, nameOf, options }
}

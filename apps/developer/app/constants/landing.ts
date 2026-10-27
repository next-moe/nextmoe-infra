export const LANDING_SAMPLE_PATH =
  'catalog/works?refs=steam:1230140,dlsite:VJ014002,bangumi:297264'

export const LANDING_ACCESS_TIERS = [
  {
    icon: 'lucide:lock-open',
    name: '匿名',
    credential: '不带任何凭据',
    limit: '30 次/分 · 1,000 次/日',
    counted: '按 IPv4 地址或 IPv6 /64 计',
    body: '整个目录只读面都能调，返回和带密钥时逐字节相同，R18 也照样能读。个人脚本、浏览器扩展、本地游戏管理器直接用就行。',
    link: { to: '/docs/quickstart', label: '快速上手' }
  },
  {
    icon: 'lucide:key-round',
    name: '应用密钥',
    credential: 'Bearer nmk_live_…',
    limit: '60 次/分 · 50,000 次/日',
    counted: '按应用计',
    body: '登录后在控制台自助铸造，不审核、不收费。从自己的服务端调用时用它，免得所有用户挤进服务器那一个 IP 的匿名额度。',
    link: { to: '/dashboard', label: '去控制台铸造' }
  },
  {
    icon: 'lucide:user-round-check',
    name: '用户访问令牌',
    credential: 'OAuth 授权码 + PKCE',
    limit: '100 次/分 · 10,000 次/日',
    counted: '按用户计',
    body: '代表某个用户读写他自己的东西：游玩时长、收藏夹、编辑提案。分发出去的桌面客户端也可以用它读目录。',
    link: { to: '/docs/user-data', label: '代表用户读写' }
  }
] as const

export const LANDING_APIS = [
  {
    icon: 'lucide:layers',
    title: '多源数据',
    prefix: '/v2/catalog',
    body: '作品、角色、厂牌与人物在六个上游的资料对齐成一条记录，逐字段给出裁定后的答案，并标明取自哪个源。',
    to: '/docs/v2'
  },
  {
    icon: 'lucide:user-round-check',
    title: '用户数据',
    prefix: '/v2/me',
    body: '代表用户读写评分、游玩时长、游玩状态与收藏夹，全网一人一份，在哪个站写的都是同一份。',
    to: '/docs/user-data'
  },
  {
    icon: 'lucide:package',
    title: 'moyu 补丁',
    prefix: '/v2/moyu',
    body: '一部游戏在鲲 Galgame 补丁上有哪些汉化、修正与存档等补丁资源，可批量反查、可增量同步。',
    to: '/docs/moyu-patches'
  },
  {
    icon: 'lucide:download',
    title: 'letmoe 下载资源链接',
    prefix: '/v2/letmoe',
    body: '一部游戏在一起萌上能下载到哪些资源；获得授权的站点可以直接取下载链接。',
    to: '/docs/letmoe'
  },
  {
    icon: 'lucide:shopping-bag',
    title: 'DLsite 分销',
    prefix: '/v2/store',
    body: '为你的站点铸专属的 DLsite 购买短链，按读者的点击分得 DLsite 返还的优惠券。',
    to: '/docs/store'
  },
  {
    icon: 'lucide:smile',
    title: 'sticker 表情包',
    prefix: '/v2/sticker',
    body: '按 catalog 作品与角色索引的 Galgame 表情包素材，一个角色有哪些表情一次查到。',
    to: '/docs/sticker-packs'
  }
] as const

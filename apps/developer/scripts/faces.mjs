// Face definitions and portal information architecture for the /docs reference.
//
// Split out of sync-specs.mjs when that file also grew the llms.txt / page-Markdown
// generators: this half is pure declaration (which prefix is which face, which
// credential it takes, which group each operation lands in), the other half is
// the machinery that reads the specs. Both generators import from here, so the
// reference pages and the LLM-facing text can never describe different faces.
//
// Wave R3 (2026-08-27) retired the five v1 faces (catalog, playtime, edit,
// news, store) with the code that served them, leaving v2 as the only
// first-party face; moyu and sticker joined later as federated downstream
// faces, served by their own repos behind the same gateway.
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
export const REPO_ROOT = join(__dirname, '..', '..', '..')

export const API_HOST = 'https://api.nextmoe.dev'

export const V2_SPEC = join(REPO_ROOT, 'docs/catalog/v2-openapi.yaml')
export const MOYU_SPEC = join(REPO_ROOT, 'docs/downstream/moyu-openapi.yaml')
export const STICKER_SPEC = join(
  REPO_ROOT,
  'docs/downstream/sticker-openapi.yaml'
)

export const PORTAL_URL = 'https://developer.nextmoe.dev'

// PR #168 (2026-09-08): the downstream faces take any valid key with no scope.
// There is no `moyu:read` / `sticker:read` — the strings do not exist and a mint
// asking for one is refused, so this face must never print a scope.
export const DOWNSTREAM_KEY_AUTH = {
  kind: 'api_key',
  curl: 'Authorization: Bearer nmk_live_<YOUR_KEY>',
  display: 'Authorization: Bearer nmk_live_…',
  note: '任意有效 v2 应用密钥即可,无需任何 scope'
}

// A face is a path prefix plus the credential that prefix accepts. The spec
// itself carries neither: OpenAPI security schemes are not emitted by the
// public gen target, and scope lives in prose. Both are derived here, from the
// prefix and the method, and they are the only place the reference pages learn
// which credential to print.
//
// specUrl is the face's live machine-readable contract, listed in llms.txt and
// on the docs pages.
export const FACES = [
  {
    key: 'v2',
    label: 'API v2',
    name: 'Public API v2',
    file: V2_SPEC,
    prefix: '/v2',
    specUrl: `${API_HOST}/v2/catalog/openapi.json`,
    scope: (method, path) => {
      if (!path) return 'catalog:read'
      // The one scoped family on /v2/me: reads take either folder scope, every
      // other method takes folder:write.
      if (path.startsWith('/v2/me/folders')) {
        return method === 'get' ? 'folder:read 或 folder:write' : 'folder:write'
      }
      if (path.startsWith('/v2/me/') || path.startsWith('/v2/moderation/')) return ''
      if (
        path.startsWith('/v2/problems') ||
        path.startsWith('/v2/vocabularies') ||
        path.startsWith('/v2/news') ||
        path.startsWith('/v2/store/prices') ||
        path === '/v2/catalog/stats' ||
        path.startsWith('/v2/catalog/schemas/')
      ) {
        return ''
      }
      if (path.startsWith('/v2/store/')) return 'store:read'
      if (path === '/v2/catalog/claim-events') return 'catalog:read + claim_events:read'
      return 'catalog:read'
    },
    auth: {
      kind: 'api_key',
      curl: 'Authorization: Bearer nmk_live_<YOUR_KEY>',
      display: 'Authorization: Bearer nmk_live_…',
      note: 'v2 应用密钥,门户自助铸造,无需申请'
    },
    autoGroups: [
      { key: 'meta', label: '注册表', match: /^\/v2\/(problems|vocabularies)/ },
      { key: 'catalog', label: '目录', match: /^\/v2\/catalog/ },
      { key: 'news', label: '资讯', match: /^\/v2\/news/ },
      { key: 'folders', label: '公开收藏夹', match: /^\/v2\/folders/ },
      { key: 'me', label: '我的', match: /^\/v2\/me/ },
      { key: 'moderation', label: '审核', match: /^\/v2\/moderation/ },
      { key: 'store', label: '商店', match: /^\/v2\/store/ }
    ],
    notes: [
      '正式公开：形状按 additive-only 演进，删除与改名由 CI 的 oasdiff 门拦下。第三方在门户自助铸 nmk_ 密钥即可调用，不需要申请。',
      '/v2/me/playtimes 只要用户令牌，不需要 playtime:read / playtime:write。任何已开通用户登录的应用都可以调用。',
      '/v2/me/work-states 与 playtimes 同款：只要用户令牌，不需要任何 scope。state 五值与 Bangumi 收藏类型一一对应，completion 表示通了多少，wish 不允许携带。',
      '/v2/me/folders 是例外：收藏夹是私人清单，读要 folder:read（folder:write 也算），写要 folder:write，缺了是 403 SCOPE_REQUIRED。',
      '/v2/folders 是别人的公开收藏夹，只要 catalog:read —— folder:read 是「读我自己的」，与能不能看别人无关。私密收藏夹在这里一律 404（对夹主本人也是），要读自己的私密夹走 /v2/me/folders。',
      '错误体是 RFC 9457 application/problem+json。type URI 解析到本站 /problems/{domain}/{kebab-code}。',
      '客户端必须忽略未知字段、容忍开放词表中未见过的取值，并为未知错误 code 准备一个按 HTTP status 的兜底分支。'
    ]
  },
  {
    key: 'moyu',
    label: 'moyu 补丁',
    name: 'moyu 补丁面',
    file: MOYU_SPEC,
    prefix: '/v2/moyu',
    specUrl: `${PORTAL_URL}/specs/moyu-openapi.yaml`,
    scope: () => '',
    auth: DOWNSTREAM_KEY_AUTH,
    autoGroups: [
      { key: 'patches', label: '补丁页', match: /^\/v2\/moyu\/patches/ },
      { key: 'resources', label: '资源', match: /^\/v2\/moyu\/resources/ }
    ],
    notes: [
      '下游站点面：由 鲲 Galgame 补丁（www.moyu.moe）自己的服务提供，经平台网关联邦进来。契约由该站的仓库拥有，本站只镜像它的 OpenAPI 原文。',
      '它只回答一件事：这部游戏在 moyu 上有哪些补丁资源。任意有效 nmk_ 应用密钥都能调，不需要任何 scope——网关只做身份、计量与限流，不做授权。',
      '不带下载直链、提取码与解压密码，这是契约里写死的取舍而不是遗漏：在 moyu 上取链接是一次单独的、按资源限速的请求，就是为了不能被批量抓走。每行都给 web_url，把读者送过去。',
      '也不带游戏名、封面、标签与角色：那些归 catalog。每行都带 catalog_work_id，拿它去 /v2/catalog/works/{id} 解析——同一把密钥就能读。patch.id、vndb_id 与 catalog_work_id 是三个互不相等的 id 空间。',
      '/v2/moyu/patches 上的 refs= 是批量反查：一次最多 100 个 vndb:<id> 或 catalog:<id> 锚，没命中的原样回在 missing[] 里。这是「我手上这 100 部作品哪些有补丁」的一次性问法，此时 cursor 与 sort 会被拒。',
      '错误方言分两半：401（缺密钥或密钥无效）与 429（超限）由网关写，body 是平台信封 {"code":10001,"message":"未授权，请先登录"}；其余全部是 RFC 9457 application/problem+json。按 HTTP status 分支，不要按 body 形状猜。',
      '只能服务端调用。网关对包括 OPTIONS 在内的每个方法都验密钥，而预检不带认证头，所以浏览器直连必然 401——nmk_ 密钥本来也不该出现在浏览器里。'
    ]
  },
  {
    key: 'sticker',
    label: 'sticker 表情包',
    name: 'sticker 表情包面',
    file: STICKER_SPEC,
    prefix: '/v2/sticker',
    specUrl: `${PORTAL_URL}/specs/sticker-openapi.yaml`,
    scope: () => '',
    auth: DOWNSTREAM_KEY_AUTH,
    autoGroups: [
      { key: 'packs', label: '表情包', match: /^\/v2\/sticker\/packs/ },
      { key: 'stickers', label: '表情', match: /^\/v2\/sticker\/stickers/ },
      { key: 'characters', label: '角色', match: /^\/v2\/sticker\/characters/ },
      { key: 'works', label: '作品', match: /^\/v2\/sticker\/works/ },
      { key: 'tags', label: '标签', match: /^\/v2\/sticker\/tags/ }
    ],
    notes: [
      '下游站点面：由 sticker.kungal.com 自己的服务提供，经平台网关联邦进来。契约由该站的仓库拥有，本站只镜像它的 OpenAPI 原文。',
      '任意有效 nmk_ 应用密钥都能调，不需要任何 scope。不要在铸密钥时去找 sticker:read——这个 scope 字符串不存在，勾了会被拒。',
      '它不是第二份内容列表：站上每张表情都打了 catalog 的作品 id 与角色 id，所以这个面回答的是「这个 catalog 身份有哪些表情素材」。/v2/sticker/characters/{character_id}/stickers 与 /v2/sticker/works/{work_id}/packs 是主车道，其余是它们的脚手架。',
      '只暴露已发布的表情包。草稿、隐藏、已删除、评论正文、审核状态与全部创作端点都不在这个面里，将来也不会有。',
      '名字是多语言映射而不是字符串：键为 zh-cn / zh-tw / ja-jp / en-us / und，每个都可缺，und 放的是 catalog 里没有语言标记的显示名。回退链由你自己定，这个面永远不替你挑。',
      '翻页用 page + limit（1-50），与 catalog 的游标翻页不同；nsfw 说的是表情包自己的分级，不是它所属游戏的——从 r18 游戏里剪出来的日常表情包是 all_ages，而它的 work.content_rating 仍然是 r18。',
      '错误方言分两半：401 与 429 由网关写，body 是平台信封 {"code":10001,"message":"未授权，请先登录"}；其余全部是 RFC 9457 application/problem+json，code 取自平台那份封闭注册表。按 HTTP status 分支。',
      '只能服务端调用：预检 OPTIONS 不带认证头，会被网关 401，浏览器直连没有出路。'
    ]
  }
]

export const NO_AUTH = {
  kind: 'none',
  curl: '',
  display: '无需凭据',
  note: '匿名可调 —— 不需要 API 密钥'
}

export const USER_TOKEN_AUTH = {
  kind: 'user_token',
  curl: 'Authorization: Bearer <ACCESS_TOKEN>',
  display: 'Authorization: Bearer <用户访问令牌>',
  note: '用户授权后的访问令牌,不是 API 密钥'
}

export const EXPECTED_OPERATION_COUNTS = {
  v2: 113,
  moyu: 4,
  sticker: 9
}

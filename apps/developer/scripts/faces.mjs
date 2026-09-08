// Face definitions and portal information architecture for the /docs reference.
//
// Split out of sync-specs.mjs when that file also grew the llms.txt / page-Markdown
// generators: this half is pure declaration (which prefix is which face, which
// credential it takes, which group each operation lands in), the other half is
// the machinery that reads the specs. Both generators import from here, so the
// reference pages and the LLM-facing text can never describe different faces.
//
// Wave R3 (2026-08-27) retired the five v1 faces (catalog, playtime, edit,
// news, store) with the code that served them, leaving v2 as the only face.
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))
export const REPO_ROOT = join(__dirname, '..', '..', '..')

export const API_HOST = 'https://api.nextmoe.dev'

export const V2_SPEC = join(REPO_ROOT, 'docs/catalog/v2-openapi.yaml')

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
  v2: 113
}

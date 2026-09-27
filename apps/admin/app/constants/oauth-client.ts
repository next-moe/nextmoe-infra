import { API_CODE_VALIDATION_FAILED } from '~/constants/devapi'

// This console and the developer console delete the same oauth_clients row
// through the same guard, and a refusal can come from any of its conditions.
// Name the possibilities rather than re-derive the rule here: a second copy of
// it would drift from the one that actually runs.
export const oauthClientDeleteMessage = (
  code: number,
  fallback: string
): string => {
  if (code !== API_CODE_VALIDATION_FAILED) return fallback || '删除失败'
  return '该客户端不可删除：它能用于用户登录、绑定了站点，或者名下还有开发者密钥、调用记录、商店短链或未过期的会话。若它是开发者应用，请先在开发者控制台归档。'
}

export type OAuthClientView =
  'mine' | 'all' | 'service' | 'developer' | 'storage' | `site-${number}`

export const OAUTH_CLIENT_VIEWS: Record<
  Exclude<OAuthClientView, `site-${number}`>,
  { label: string; icon: string; description: string }
> = {
  mine: {
    label: '我创建的',
    icon: 'lucide:user-round',
    description: '你在控制台创建的客户端，以及你在开发者平台注册的应用'
  },
  all: {
    label: '全部',
    icon: 'lucide:layers',
    description: '所有客户端，按站点分组'
  },
  service: {
    label: '内部服务',
    icon: 'lucide:server',
    description:
      '不绑定站点、不用于用户登录的服务间客户端，例如图片上传、审核转发'
  },
  developer: {
    label: '开发者应用',
    icon: 'lucide:terminal',
    description:
      '第三方开发者在开发者平台注册的应用；等级、密钥与审核在「开发者平台」管理'
  },
  storage: {
    label: '已开通存储',
    icon: 'lucide:hard-drive',
    description: '开通了图片或文件上传的客户端'
  }
}

// The image service's presets are defined in apps/api/configs/image_presets.yaml;
// a preset missing here still shows and saves, it just has no Chinese label.
export const IMAGE_PRESET_LABELS: Record<string, string> = {
  avatar: '头像',
  topic: '帖子配图',
  message: '私信图片',
  sticker: '表情包',
  galgame_banner: 'Wiki 横幅',
  galgame_screenshot: 'Wiki 截图',
  character: '角色头像',
  character_figure: '角色立绘',
  catalog_cover: '目录封面',
  catalog_screenshot: '目录截图',
  catalog_logo: '目录 Logo',
  news_banner: '情报横幅'
}

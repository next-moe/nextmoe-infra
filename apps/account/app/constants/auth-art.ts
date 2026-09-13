export interface AuthArt {
  src: string
  headline: string
  caption?: string
}

/*
 * One illustration per auth surface, each chosen for what the page does: the
 * outstretched hand invites, the wave greets, the lantern is released, the
 * mirror shows the same person twice. Every pose faces the viewer's left, which
 * is why the art panel sits on the right — the figure leans toward the form.
 */
export const AUTH_ART = {
  login: {
    src: '/mascot/koi-login.webp',
    headline: '一个账号，通行所有站点',
    caption: '论坛、补丁、图鉴、表情包，登录一次就够了。'
  },
  register: {
    src: '/mascot/koi-register.webp',
    headline: '从这里开始',
    caption: '创建 NextMoe 账号，你的收藏与足迹会跟着你走。'
  },
  authorize: {
    src: '/mascot/koi-authorize.webp',
    headline: '你决定交出什么',
    caption: '每一项权限都写在下面，随时可以收回。'
  },
  forgot: {
    src: '/mascot/koi-forgot.webp',
    headline: '信会寄到你手上',
    caption: '重置链接只发往账号绑定的邮箱。'
  },
  reset: {
    src: '/mascot/koi-reset.webp',
    headline: '换一把新钥匙',
    caption: '设置新密码后，其他设备需要重新登录。'
  },
  federation: {
    src: '/mascot/koi-federation.webp',
    headline: '同一个你',
    caption: '第三方身份与 NextMoe 账号在此合为一个。'
  },
  farewell: {
    src: '/mascot/koi-farewell.webp',
    headline: '下次见',
    caption: '正在清除这台设备上的登录状态。'
  }
} satisfies Record<string, AuthArt>

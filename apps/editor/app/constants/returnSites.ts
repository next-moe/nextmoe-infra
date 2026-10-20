export interface ReturnSite {
  key: string
  label: string
  origins: string[]
}

export const RETURN_SITES: ReturnSite[] = [
  {
    key: 'kungal',
    label: '鲲 Galgame 论坛',
    origins: ['https://www.kungal.com', 'https://kungal.com']
  },
  {
    key: 'moyu',
    label: '鲲 Galgame 补丁',
    origins: ['https://www.moyu.moe', 'https://moyu.moe']
  },
  {
    key: 'letmoe',
    label: 'LetMoe',
    origins: [
      'https://www.letmoe.com',
      'https://letmoe.com',
      'https://staging.letmoe.com'
    ]
  },
  {
    key: 'nextmoe',
    label: 'NextMoe',
    origins: ['https://www.nextmoe.com', 'https://nextmoe.com']
  }
]

export const RETURN_LINK_STORAGE_KEY = 'nextmoe-editor-return'

import type { EditFamily } from './editEntities'

export type PictureKey = 'image' | 'photo' | 'logo' | 'cover'

export const LIST_PICTURE_INCLUDE: Partial<
  Record<EditFamily, { include: string; key: PictureKey }>
> = {
  company: { include: 'logo', key: 'logo' },
  character: { include: 'image', key: 'image' },
  credit_name: { include: 'photo', key: 'photo' }
}

export const PICTURE_KEYS: PictureKey[] = ['image', 'photo', 'logo', 'cover']

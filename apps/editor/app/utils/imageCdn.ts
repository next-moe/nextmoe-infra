export const DEFAULT_IMAGE_CDN_BASE = 'https://image.kungal.iloveren.link'

let imageCdnBase = DEFAULT_IMAGE_CDN_BASE

export const setImageCdnBase = (base: string | undefined) => {
  imageCdnBase = (base || DEFAULT_IMAGE_CDN_BASE).replace(/\/+$/, '')
}

export const imageUrl = (hash: unknown) =>
  typeof hash === 'string' && hash.length >= 4
    ? `${imageCdnBase}/${hash.slice(0, 2)}/${hash.slice(2, 4)}/${hash}.webp`
    : ''

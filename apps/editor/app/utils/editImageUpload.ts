import type { CatalogEditImage } from '#shared/types/edit'

export const EDIT_IMAGE_ACCEPT =
  'image/png,image/jpeg,image/webp,image/avif,image/gif'
export const EDIT_IMAGE_MAX_BYTES = 20 * 1024 * 1024

type EditorFetch = <T>(
  url: string,
  options: { method: 'POST'; body: FormData }
) => Promise<T>

export const uploadEditImage = (
  api: EditorFetch,
  preset: 'cover' | 'screenshot' | 'photo',
  file: File
) => {
  const form = new FormData()
  form.append('preset', preset)
  form.append('file', file)
  return api<CatalogEditImage>('/v2/me/edit-images', {
    method: 'POST',
    body: form
  })
}

export const isImageFile = (file: File) =>
  EDIT_IMAGE_ACCEPT.split(',').includes(file.type)

export const hasDraggedFiles = (event: DragEvent) =>
  Array.from(event.dataTransfer?.types ?? []).includes('Files')

const codePoints = (value: string) =>
  Array.from(value, (char) => char.codePointAt(0) ?? 0)

export const compareCodePoints = (a: string, b: string) => {
  const left = codePoints(a)
  const right = codePoints(b)
  for (let i = 0; i < Math.min(left.length, right.length); i++) {
    if (left[i] !== right[i]) {
      return left[i]! - right[i]!
    }
  }
  return left.length - right.length
}

export const sortedIdentities = (keys: Iterable<string>) =>
  [...new Set(keys)].sort(compareCodePoints)

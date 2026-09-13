interface UploadResponse<T> {
  code: number
  message: string
  data?: T
}

// Multipart, so it cannot go through useApi (that one JSON-encodes every body).
export const useImageUpload = () => {
  const uploading = ref(false)
  const error = ref('')

  const upload = async <T extends object>(
    endpoint: string,
    blob: Blob,
    filename: string
  ): Promise<T | null> => {
    if (uploading.value) return null
    uploading.value = true
    error.value = ''

    try {
      const fd = new FormData()
      fd.append('file', blob, filename)

      const cookie = useCookie('access_token')
      const res = await $fetch<UploadResponse<T>>(
        `${resolveApiBase()}${endpoint}`,
        {
          method: 'POST',
          body: fd,
          headers: cookie.value
            ? { Authorization: `Bearer ${cookie.value}` }
            : {},
          credentials: 'include',
        }
      )

      if (res.code === 0 && res.data) {
        return res.data
      }
      error.value = res.message || '上传失败'
      return null
    } catch (err) {
      const e = err as {
        data?: { message?: string }
        statusMessage?: string
        message?: string
      }
      error.value =
        e?.data?.message || e?.statusMessage || e?.message || '网络错误'
      return null
    } finally {
      uploading.value = false
    }
  }

  return { uploading, error, upload }
}

interface ReadFailure {
  statusCode?: number
  data?: { detail?: string; title?: string; message?: string }
  response?: { headers: Headers }
}

export const useCatalogRead = () => {
  const base = useRuntimeConfig().public.nextmoeApiBase

  const read = <T>(
    path: string,
    query: Record<string, string>,
    apiKey: string
  ): Promise<T> => {
    const key = apiKey.trim()
    return $fetch<T>(`${base}/${path}`, {
      query,
      headers: key ? { Authorization: `Bearer ${key}` } : {}
    })
  }

  const failureMessage = (e: unknown, apiKey: string): string => {
    const err = e as ReadFailure
    if (err.statusCode === 429 && !apiKey.trim()) {
      const retry = err.response?.headers.get('retry-after')
      return `这个 IP 的匿名额度用完了。${retry ? `${retry} 秒后` : '稍后'}再试，或者在上方填入你自己的 key。`
    }
    return (
      err.data?.detail ??
      err.data?.title ??
      err.data?.message ??
      `请求失败（${err.statusCode ?? '网络错误'}）`
    )
  }

  return { read, failureMessage }
}

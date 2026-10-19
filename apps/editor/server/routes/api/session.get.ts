import type { EditorSession } from '#shared/types/session'

interface UserInfo {
  picture?: string
  name?: string
}

interface CapabilitiesWire {
  fields?: { can_review?: boolean }[]
}

const getJson = async <T>(url: string, token: string): Promise<T | null> => {
  try {
    const res = await fetch(url, {
      headers: { authorization: `Bearer ${token}`, accept: 'application/json' }
    })
    return res.ok ? ((await res.json()) as T) : null
  } catch {
    return null
  }
}

export default defineEventHandler(
  async (
    event
  ): Promise<EditorSession | ReturnType<typeof respondSessionExpired>> => {
    const token = await ensureEditorSession(event)
    const claims = token ? decodeAccessToken(token) : null
    if (!token || !claims) {
      return respondSessionExpired(event)
    }

    const config = editorServerConfig(event)
    const [info, caps] = await Promise.all([
      getJson<UserInfo>(`${config.tokenBase}/oauth/userinfo`, token),
      getJson<CapabilitiesWire>(
        `${config.catalogBase}/v2/me/edit-capabilities/work`,
        token
      )
    ])

    setResponseHeader(event, 'cache-control', 'no-store')
    return {
      id: claims.id,
      uuid: claims.sub,
      name: info?.name || claims.name,
      avatar: info?.picture || '',
      roles: claims.roles,
      siteRoles: claims.siteRoles,
      canReview: caps?.fields?.some((f) => f.can_review === true) ?? false
    }
  }
)

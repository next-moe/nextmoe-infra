export const DISCUSSION_MAX_BODY = 2000
export const DISCUSSION_MAX_MENTIONS = 10

export const DISCUSSION_ANCHOR_LABELS: Record<string, string> = {
  proposal: '提案',
  merge_request: '合并请求',
  deletion_request: '删除请求'
}

export const DISCUSSION_OWNER_LABELS: Record<string, string> = {
  proposal: '提案人',
  merge_request: '发起人',
  deletion_request: '发起人'
}

type AnchorRoute = (id: string, viewerIsOwner: boolean) => string

export const DISCUSSION_ANCHOR_ROUTES: Record<string, AnchorRoute> = {
  proposal: (id, owner) => (owner ? `/me/proposals/${id}` : `/review/${id}`),
  merge_request: (_, owner) =>
    owner ? '/me/merge-requests' : '/review/merges',
  deletion_request: (id, owner) =>
    owner ? `/me/deletion-requests?id=${id}` : `/review/deletions?id=${id}`
}

export const discussionAnchorRoute = (
  kind: string,
  id: string,
  viewerIsOwner: boolean
) => {
  const route = DISCUSSION_ANCHOR_ROUTES[kind]
  return route ? `${route(id, viewerIsOwner)}#discussion` : null
}

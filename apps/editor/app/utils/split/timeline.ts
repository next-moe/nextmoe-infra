import type { CatalogSplitRequest } from '#shared/types/catalog'
import { requestTimeline, type TimelineWording } from '~/utils/merge/timeline'

export const SPLIT_WORDING: TimelineWording = {
  execute: '执行拆分',
  executeDetail: '冷静期结束后自动执行，审核者也可以手动执行',
  done: '已拆分',
  undo: '撤销拆分',
  undoneDetail: '审核者撤销了这次拆分，发行已回到原作品',
  undoWindowDetail: '期间审核者可以撤销，把新作品并回原作品'
}

export const splitTimeline = (request: CatalogSplitRequest, now: number) =>
  requestTimeline(request, now, SPLIT_WORDING)

export const splitParticipants = (request: CatalogSplitRequest) => [
  { uid: request.proposed_by_uid, user: request.proposed_by, role: '发起人' },
  ...(request.amended_by_uid
    ? [
        {
          uid: request.amended_by_uid,
          user: request.amended_by,
          role: '修改人'
        }
      ]
    : []),
  ...(request.decided_by_uid
    ? [
        {
          uid: request.decided_by_uid,
          user: request.decided_by,
          role: '审核人'
        }
      ]
    : []),
  ...(request.undone_by_uid
    ? [{ uid: request.undone_by_uid, user: request.undone_by, role: '撤销人' }]
    : [])
]

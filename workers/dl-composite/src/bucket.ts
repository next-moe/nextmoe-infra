import { AwsClient } from 'aws4fetch'
import type { Env } from './env'
import type { RangeFetcher } from './stream'

export interface Bucket {
  get: (key: string) => Promise<Response>
  range: RangeFetcher
}

// cache: 'no-store' keeps Cloudflare's cache out of the way. Through the
// public dl.imoe.uk path, the first Range request for an object the edge had
// not cached came back 200 with the whole object (letmoe 2026-10-13 spec §1);
// a splice must get exactly the bytes it asked for, so anything but the
// matching 206 is an error here.
export const bucket = (env: Env): Bucket => {
  const aws = new AwsClient({
    accessKeyId: env.B2_KEY_ID,
    secretAccessKey: env.B2_APP_KEY,
    service: 's3',
    region: env.B2_REGION,
    retries: 2,
  })
  const url = (key: string): string =>
    `${env.B2_ENDPOINT}/${env.B2_BUCKET}/${key.split('/').map(encodeURIComponent).join('/')}`

  return {
    get: (key) => aws.fetch(url(key), { cache: 'no-store' }),
    range: async (key, offset, length) => {
      const last = offset + length - 1
      const res = await aws.fetch(url(key), { headers: { Range: `bytes=${offset}-${last}` }, cache: 'no-store' })
      const contentRange = res.headers.get('Content-Range') ?? ''
      if (res.status !== 206 || !contentRange.startsWith(`bytes ${offset}-${last}/`) || !res.body) {
        await res.body?.cancel()
        throw new Error(`b2 ${key} [${offset}, +${length}) answered ${res.status} ${contentRange}`)
      }
      return res.body
    },
  }
}

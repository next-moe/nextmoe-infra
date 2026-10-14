import type { Item, Step } from './plan'

export type RangeFetcher = (key: string, offset: number, length: number) => Promise<ReadableStream<Uint8Array>>

const copyItems = async (
  body: ReadableStream<Uint8Array>,
  offset: number,
  items: Item[],
  writer: WritableStreamDefaultWriter<Uint8Array>,
): Promise<void> => {
  const reader = body.getReader()
  let buf: Uint8Array = new Uint8Array(0)
  let pos = offset
  const take = async (n: number, write: boolean): Promise<void> => {
    while (n > 0) {
      if (buf.length === 0) {
        const r = await reader.read()
        if (r.done) throw new Error(`source ended at ${pos}, ${n} bytes short`)
        buf = r.value
      }
      const k = Math.min(n, buf.length)
      if (write) await writer.write(buf.subarray(0, k))
      buf = buf.subarray(k)
      pos += k
      n -= k
    }
  }
  try {
    for (const item of items) {
      if ('mem' in item) {
        await writer.write(item.mem)
        continue
      }
      await take(item.at - pos, false)
      await take(item.length, true)
    }
  } finally {
    await reader.cancel().catch(() => {})
  }
}

export const pump = async (steps: Step[], writable: WritableStream<Uint8Array>, fetchRange: RangeFetcher): Promise<void> => {
  let writer: WritableStreamDefaultWriter<Uint8Array> | null = writable.getWriter()
  try {
    for (const step of steps) {
      if ('mem' in step) {
        await writer.write(step.mem)
        continue
      }
      const body = await fetchRange(step.key, step.offset, step.length)
      const only = step.items.length === 1 ? step.items[0]! : null
      if (only && 'at' in only && only.at === step.offset && only.length === step.length) {
        writer.releaseLock()
        writer = null
        await body.pipeTo(writable, { preventClose: true })
        writer = writable.getWriter()
        continue
      }
      await copyItems(body, step.offset, step.items, writer)
    }
    await writer.close()
  } catch (err) {
    await (writer ?? writable).abort(err).catch(() => {})
    throw err
  }
}

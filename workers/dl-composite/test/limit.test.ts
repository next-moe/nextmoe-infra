import { describe, expect, it } from 'vitest'
import worker, { clientKey } from '../src/index'
import type { Env } from '../src/env'

describe('clientKey', () => {
  it('keeps an IPv4 address whole', () => {
    expect(clientKey('203.0.113.7')).toBe('203.0.113.7')
  })
  it('folds an IPv6 address to its /64', () => {
    expect(clientKey('2001:db8:85a3:42:1:2:3:4')).toBe('2001:db8:85a3:42::/64')
    expect(clientKey('2001:DB8:0085:0042:ffff::1')).toBe('2001:db8:85:42::/64')
  })
  it('expands :: before taking the prefix', () => {
    expect(clientKey('2001:db8::1')).toBe('2001:db8:0:0::/64')
    expect(clientKey('::1')).toBe('0:0:0:0::/64')
  })
})

describe('per-IP limit', () => {
  const url = 'https://dl.imoe.uk/c/letmoe/0123456789abcdef0123456789abcdef.zip'
  const call = (success: boolean, keys: string[] = []) => {
    const env = {
      PER_IP: {
        limit: async ({ key }: { key: string }) => {
          keys.push(key)
          return { success }
        },
      },
    } as unknown as Env
    const req = new Request(url, { headers: { 'CF-Connecting-IP': '2001:db8:1:2:3:4:5:6' } })
    return worker.fetch(req as never, env, {} as never)
  }

  it('answers 429 with Retry-After before touching the bucket', async () => {
    const keys: string[] = []
    const res = await call(false, keys)
    expect(res.status).toBe(429)
    expect(res.headers.get('Retry-After')).toBe('60')
    expect(keys).toEqual(['2001:db8:1:2::/64'])
  })

  it('does not count a path it would 404 anyway', async () => {
    const keys: string[] = []
    const env = { PER_IP: { limit: async () => (keys.push('x'), { success: false }) } } as unknown as Env
    const res = await worker.fetch(new Request('https://dl.imoe.uk/c/nope') as never, env, {} as never)
    expect(res.status).toBe(404)
    expect(keys).toEqual([])
  })
})

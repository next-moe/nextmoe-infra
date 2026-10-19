import { createHash, randomBytes } from 'node:crypto'

const base64Url = (buf: Buffer) =>
  buf
    .toString('base64')
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '')

export const createCodeVerifier = () => base64Url(randomBytes(32))

export const codeChallengeFor = (verifier: string) =>
  base64Url(createHash('sha256').update(verifier).digest())

export const createOAuthState = () => randomBytes(16).toString('hex')

export const isOAuthState = (value: unknown): value is string =>
  typeof value === 'string' && /^[0-9a-f]{32}$/.test(value)

import { mkdirSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { account } from '../app/config/account'

const __dirname = dirname(fileURLToPath(import.meta.url))
const publicDir = join(__dirname, '..', 'public')

const paths = ['/', '/auth/login', '/auth/register', '/auth/forgot-password']

const urlEntries = paths
  .map((path) => {
    const loc = `${account.canonical}${path === '/' ? '/' : path}`
    const priority = path === '/' ? '1.0' : '0.8'
    return `  <url>
    <loc>${loc}</loc>
    <changefreq>monthly</changefreq>
    <priority>${priority}</priority>
  </url>`
  })
  .join('\n')

const xml = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${urlEntries}
</urlset>
`

mkdirSync(publicDir, { recursive: true })
writeFileSync(join(publicDir, 'sitemap.xml'), xml)
writeFileSync(
  join(publicDir, 'urls.txt'),
  paths.map((path) => `${account.canonical}${path === '/' ? '/' : path}`).join('\n') +
    '\n'
)

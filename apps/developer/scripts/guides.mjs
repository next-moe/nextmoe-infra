// Information architecture for the hand-written half of /docs.
//
// The API reference (faces.mjs) is generated from the OpenAPI spec and answers
// "what does this endpoint do". These guides answer everything a reference
// cannot: how to start, what the data means, what the cross-cutting protocol
// rules are, and how to build the three integrations people actually build.
//
// Sources are plain Markdown in apps/developer/docs/. scripts/build-guides.mjs
// compiles them; nothing else decides which pages exist or in what order.
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = dirname(fileURLToPath(import.meta.url))

export const GUIDES_DIR = join(__dirname, '..', 'docs')

// Order here is the order in the sidebar, in llms.txt, and in prev/next.
export const GUIDE_SECTIONS = [
  { key: 'start', label: '开始', slugs: ['quickstart', 'authentication'] },
  { key: 'concepts', label: '核心概念', slugs: ['concepts', 'design'] },
  {
    key: 'protocol',
    label: 'API 基础',
    slugs: [
      'conventions',
      'pagination',
      'shaping',
      'errors',
      'rate-limits',
      'caching',
      'versioning'
    ]
  },
  {
    key: 'guides',
    label: '集成指南',
    slugs: ['example', 'mirror', 'user-data', 'native-app', 'best-practices']
  }
]

export const GUIDE_SLUGS = GUIDE_SECTIONS.flatMap((s) => s.slugs)

export const guideRoute = (slug) => `/docs/${slug}`

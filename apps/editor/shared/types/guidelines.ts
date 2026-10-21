export type GuidelineCategory =
  'start' | 'rules' | 'work' | 'people' | 'vocab' | 'appendix'

export interface GuidelineHeading {
  id: string
  text: string
  depth: number
}

export interface GuidelineDoc {
  slug: string
  title: string
  description: string
  category: GuidelineCategory
  order: number
  icon: string
  related: string[]
  headings: GuidelineHeading[]
  text: string
  html: string
}

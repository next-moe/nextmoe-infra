export type WtNodeType = 'entry' | 'save' | 'ending' | 'collect' | 'open'
export type WtStepType = 'choice' | 'action' | 'save'
export type WtEndingType =
  | 'heroine'
  | 'harem'
  | 'true'
  | 'normal'
  | 'bad'
  | 'after'
  | 'other'
export type WtLang = 'ja' | 'zh-Hans'

export interface WtCharacter {
  key: string
  name: string
  aliases: string[]
}

export interface WtSection {
  key: string
  title: string
  title_zh: string | null
  characters: string[]
}

export interface WtEntry {
  key: string
  label: string
  unlocked_by: string | null
}

export interface WtSave {
  key: string
  label: string
}

export interface WtEnding {
  key: string
  name: string
  name_zh: string | null
  ending_type: WtEndingType
  characters: string[]
}

export interface WtNode {
  node_type: WtNodeType
  key: string | null
}

export interface WtStep {
  step_type: WtStepType
  text: string | null
  text_zh: string | null
  alt: string[]
  alt_zh: (string | null)[]
  achievement: boolean
  save: string | null
}

export interface WtSegment {
  key: string
  section: string | null
  from: WtNode
  condition: string | null
  condition_zh: string | null
  steps: WtStep[]
  to: WtNode
}

export interface WtChart {
  characters: WtCharacter[]
  sections: WtSection[]
  entries: WtEntry[]
  saves: WtSave[]
  endings: WtEnding[]
  segments: WtSegment[]
}

export interface WtPin {
  chart_key: string
  character_id: number
}

export type WtTarget =
  | { kind: 'character' | 'section' | 'entry' | 'save' | 'ending'; key: string }
  | { kind: 'segment'; key: string; step?: number }

export interface WtIssue {
  target: WtTarget | null
  message: string
}

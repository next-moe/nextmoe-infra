import { describe, expect, it } from 'vitest'
import {
  discussionSnippet,
  mentionsInBody,
  renderDiscussionBody
} from '../app/utils/discussion'
import { discussionAnchorRoute } from '../app/constants/discussion'

describe('renderDiscussionBody', () => {
  it('escapes markup instead of rendering it', () => {
    const html = renderDiscussionBody('<img src=x onerror=alert(1)> & "q"')
    expect(html).not.toContain('<img')
    expect(html).toContain(
      '&lt;img src=x onerror=alert(1)&gt; &amp; &quot;q&quot;'
    )
  })

  it('links http(s) URLs and leaves trailing punctuation outside', () => {
    const html = renderDiscussionBody(
      '见 https://vndb.org/v751。也可看 http://a.b/c?x=1&y=2.'
    )
    expect(html).toContain(
      '<a href="https://vndb.org/v751" target="_blank" rel="noopener noreferrer nofollow ugc"'
    )
    expect(html).toContain('href="http://a.b/c?x=1&amp;y=2"')
    expect(html).toMatch(/<\/a>\.$/)
  })

  it('never links javascript: or an attribute-breaking quote', () => {
    const html = renderDiscussionBody(
      'javascript:alert(1) https://x.y/"onmouseover=alert(1)'
    )
    expect(html).not.toContain('href="javascript')
    expect(html).toContain('href="https://x.y/"')
    expect(html).not.toContain('" onmouseover')
  })

  it('renders bold, inline code and mentions, and keeps code literal', () => {
    const html = renderDiscussionBody('**注意** `**不加粗** <b>` @柚原 你好')
    expect(html).toContain('<strong>注意</strong>')
    expect(html).toContain('>**不加粗** &lt;b&gt;</code>')
    expect(html).toContain(
      '<span class="text-primary-600 font-medium">@柚原</span>'
    )
  })

  it('turns newlines into breaks', () => {
    expect(renderDiscussionBody('一\n二')).toBe('一<br>二')
  })
})

describe('discussion helpers', () => {
  it('shortens snippets on one line', () => {
    expect(discussionSnippet('一二三\n四五六七', 5)).toBe('一二三 四…')
    expect(discussionSnippet('例如 **官网** 或 `v751`')).toBe(
      '例如 官网 或 v751'
    )
  })

  it('keeps only mentions still present in the text', () => {
    const people = [
      { uid: '1', name: 'kun' },
      { uid: '3', name: 'MercuryMoe' }
    ]
    expect(mentionsInBody('@MercuryMoe 请看', people)).toEqual(['3'])
  })

  it('routes notices by anchor kind and ownership', () => {
    expect(discussionAnchorRoute('proposal', '12', true)).toBe(
      '/me/proposals/12#discussion'
    )
    expect(discussionAnchorRoute('proposal', '12', false)).toBe(
      '/review/12#discussion'
    )
    expect(discussionAnchorRoute('unknown', '1', true)).toBeNull()
  })
})

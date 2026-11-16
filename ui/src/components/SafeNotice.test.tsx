import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { SafeNotice } from './SafeNotice'

afterEach(cleanup)

const html = (s: string) => {
  const { container } = render(<SafeNotice html={s} />)
  return container
}

describe('SafeNotice', () => {
  it('keeps the allowlisted markup', () => {
    const c = html('<p>Backup of <b>bob</b> <em>failed</em> <code>x</code><br><strong>now</strong> <i>i</i> <span>s</span></p>')
    for (const tag of ['p', 'b', 'em', 'code', 'br', 'strong', 'i', 'span']) expect(c.querySelector(tag), tag).not.toBeNull()
    expect(c.textContent).toContain('Backup of bob failed')
  })

  it('drops script elements and their code', () => {
    const c = html('<p>hi</p><script>window.pwned = 1</script>')
    expect(c.querySelector('script')).toBeNull()
    expect(c.textContent).toBe('hi')
  })

  it('drops event handlers and unknown elements', () => {
    const c = html('<p onclick="x()">a<img src=x onerror="window.pwned=1">b<iframe src="https://evil.example"></iframe></p>')
    expect(c.innerHTML).not.toMatch(/onerror|onclick|<img|<iframe/i)
    expect(c.textContent).toBe('ab')
  })

  it('drops javascript links but keeps their text', () => {
    for (const bad of ['javascript:alert(1)', ' JaVa\tScript:alert(1)', 'data:text/html,x', 'http://plain.example/', '//evil.example/', '/\\evil.example/', '/\t/evil.example']) {
      const c = html(`<a href="${bad}">click</a>`)
      expect(c.querySelector('a'), bad).toBeNull()
      expect(c.textContent).toBe('click')
      cleanup()
    }
  })

  it('keeps https and relative links, opened safely', () => {
    const c = html(`<a href='https://docs.example.org/'>docs</a><a href="/settings">set</a>`)
    const [a, b] = Array.from(c.querySelectorAll('a'))
    expect(a.getAttribute('href')).toBe('https://docs.example.org/')
    expect(a.getAttribute('rel')).toContain('noopener')
    expect(b.getAttribute('href')).toBe('/settings')
  })

  it('strips attributes other than href', () => {
    const c = html('<a href="https://x.example/" style="color:red" class="big" onmouseover="x()">t</a><p class="c" id="i">u</p>')
    expect(c.innerHTML).not.toMatch(/style=|class="big"|class="c"|onmouseover|id=/)
  })

  it('shows quotes stored by Hestia', () => {
    expect(html('<p>it%quote%s</p>').textContent).toBe("it's")
  })
})

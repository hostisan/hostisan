import { Fragment, useMemo, type ReactNode } from 'react'

// Notification text is HTML from the core. It is never put in innerHTML: the markup is
// parsed with DOMParser (which runs nothing) and rebuilt as React elements from an
// allowlist. Everything else is unwrapped to its text, or dropped with its content.

const INLINE = new Set(['p', 'span', 'b', 'strong', 'i', 'em', 'code'])
const DROP = new Set(['script', 'style', 'template', 'noscript', 'iframe', 'object', 'embed', 'svg', 'math', 'head', 'title'])

/** An https link or a relative one on this site; never a script, data, http or protocol-relative URL. */
function safeHref(raw: string | null): string | null {
  if (!raw) return null
  // Browsers ignore whitespace and control characters inside a scheme.
  // eslint-disable-next-line no-control-regex
  const href = raw.replace(/[\u0000- ]/g, '')
  if (/^[a-z][a-z0-9+.-]*:/i.test(href)) return /^https:/i.test(href) && href.length > 8 ? raw.trim() : null
  if (href.startsWith('//') || href.startsWith('\\') || href.startsWith('/\\')) return null
  return raw.trim()
}

function convert(node: Node, key: string, depth: number): ReactNode {
  if (node.nodeType === Node.TEXT_NODE) return (node.textContent ?? '').replaceAll('%quote%', "'")
  if (node.nodeType !== Node.ELEMENT_NODE || depth > 20) return null
  const el = node as Element
  const tag = el.tagName.toLowerCase()
  if (DROP.has(tag)) return null
  if (tag === 'br') return <br key={key} />
  const kids = Array.from(el.childNodes).map((c, i) => convert(c, `${key}.${i}`, depth + 1))
  if (INLINE.has(tag)) {
    const Tag = tag as 'p'
    return <Tag key={key}>{kids}</Tag>
  }
  if (tag === 'a') {
    const href = safeHref(el.getAttribute('href'))
    if (!href) return <Fragment key={key}>{kids}</Fragment>
    return (
      <a key={key} href={href} target="_blank" rel="noopener noreferrer" className="underline">
        {kids}
      </a>
    )
  }
  return <Fragment key={key}>{kids}</Fragment>
}

export function renderSafeNotice(html: string): ReactNode[] {
  const doc = new DOMParser().parseFromString(html, 'text/html')
  return Array.from(doc.body.childNodes).map((n, i) => convert(n, String(i), 0))
}

/** A notification's NOTICE, rendered through the allowlist. */
export function SafeNotice({ html, className }: { html: string; className?: string }) {
  const nodes = useMemo(() => renderSafeNotice(html), [html])
  return <div className={className}>{nodes}</div>
}

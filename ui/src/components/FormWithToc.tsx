import { TableOfContents, useTableOfContentsActiveId } from '@cloudflare/kumo'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useT } from '../i18n/I18nProvider'

export type TocSection = { id: string; label: string }

function scrollParent(el: HTMLElement | null): HTMLElement | null {
  for (let node = el?.parentElement; node; node = node.parentElement) {
    if (/(auto|scroll)/.test(getComputedStyle(node).overflowY)) return node
  }
  return null
}

/** Section anchor for FormWithToc. */
export function TocAnchor({ id, children }: { id: string; children: ReactNode }) {
  return (
    <div id={id} className="scroll-mt-6">
      {children}
    </div>
  )
}

/**
 * Long form with an "On this page" side nav at xl+. `onBeforeJump` lets the form open a
 * collapsed section (e.g. advanced options) before scrolling to it.
 */
export function FormWithToc({
  sections,
  onBeforeJump,
  children,
}: {
  sections: TocSection[]
  onBeforeJump?: (id: string) => void
  children: ReactNode
}) {
  const t = useT()
  const contentRef = useRef<HTMLDivElement>(null)
  const [root, setRoot] = useState<HTMLElement | null>(null)
  useEffect(() => setRoot(scrollParent(contentRef.current)), [])
  const { activeId, selectSection } = useTableOfContentsActiveId({
    ids: sections.map((s) => s.id),
    root,
    offset: 24,
    trackHash: false,
  })
  const jumpTo = (id: string) => {
    onBeforeJump?.(id)
    selectSection(id)
    requestAnimationFrame(() => document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' }))
  }

  return (
    <div ref={contentRef} className="grid gap-8 xl:grid-cols-[minmax(0,1fr)_11rem]">
      <div className="grid min-w-0 gap-8">{children}</div>
      <nav aria-label={t('On this page')} className="hidden xl:block">
        <div className="sticky top-6">
          <TableOfContents>
            <TableOfContents.Title className="normal-case">{t('On this page')}</TableOfContents.Title>
            <TableOfContents.List>
              {sections.map((s) => (
                <TableOfContents.Item
                  key={s.id}
                  active={activeId === s.id}
                  className="cursor-pointer"
                  render={<a href={`#${s.id}`} />}
                  onClick={(e: React.MouseEvent) => {
                    e.preventDefault()
                    jumpTo(s.id)
                  }}
                >
                  {s.label}
                </TableOfContents.Item>
              ))}
            </TableOfContents.List>
          </TableOfContents>
        </div>
      </nav>
    </div>
  )
}

import { Fragment, type ReactNode } from 'react'

/**
 * A translated sentence with its %s placeholders replaced by `nodes`, in order, so a
 * whole sentence stays one msgid even when part of it is a link or code:
 * interpolate(t('Files are served from %s.'), <code>{path}</code>). When the count of
 * placeholders and nodes differs, the text comes back as it is, as t() does.
 */
export function interpolate(text: string, ...nodes: ReactNode[]): ReactNode {
  const parts = text.split('%s')
  if (parts.length !== nodes.length + 1) return text
  return parts.map((part, i) => (
    <Fragment key={i}>
      {part}
      {i < nodes.length && nodes[i]}
    </Fragment>
  ))
}

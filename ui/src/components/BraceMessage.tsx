import { Fragment, type ReactNode } from 'react'

/**
 * A Hestia message whose {…} part is marked up, as htmlify_trans does: "Package {%s} has
 * been created successfully." with the braces' text passed to render (a link, say).
 * Pass the translated text; %s inside the braces is the value.
 */
export function braceMessage(text: string, value: string, render: (inner: string) => ReactNode): ReactNode {
  return (
    <>
      {text.split(/(\{[^}]*\})/).map((part, i) =>
        /^\{[^}]*\}$/.test(part) ? (
          <Fragment key={i}>{render(part.slice(1, -1).replace('%s', () => value))}</Fragment>
        ) : (
          part.replace('%s', () => value)
        ),
      )}
    </>
  )
}

import type { MouseEvent } from 'react'

/**
 * Makes a whole table row open its item, without hijacking clicks on the controls
 * inside it (checkbox, links, menus) or text selection. The row's primary cell should
 * still hold a real link, which is the keyboard and screen reader path.
 */
export function rowLink(open: () => void) {
  return {
    className: 'cursor-pointer',
    onClick: (e: MouseEvent<HTMLElement>) => {
      const target = e.target as HTMLElement
      if (target.closest('a, button, input, label, [role="checkbox"], [role="menu"], [role="menuitem"]')) return
      if (window.getSelection()?.toString()) return
      open()
    },
  }
}

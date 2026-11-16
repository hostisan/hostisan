/**
 * Where the Help item points: Hostisan's docs site. Empty until the docs exist (ruling
 * D17), which keeps the item hidden.
 */
export const docsUrl = ''

/** The Help link to show in the top bar, or null: hidden by HIDE_DOCS or without a docs URL. */
export function helpLink(hideDocs: boolean, url: string): string | null {
  return hideDocs || url === '' ? null : url
}

/**
 * The app shell is one viewport tall and the content pane scrolls, not the window, so
 * window.scrollTo does nothing inside the app. Use this to show the top of a page.
 */
export function scrollContentToTop() {
  document.querySelector('main')?.closest('.overflow-y-auto')?.scrollTo({ top: 0 })
}

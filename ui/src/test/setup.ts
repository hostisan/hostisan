import { configure } from '@testing-library/react'

// findBy*/waitFor default to 1s, which times out when the machine is busy.
configure({ asyncUtilTimeout: 3000 })

// Browser APIs Kumo uses that jsdom doesn't implement.
if (!window.matchMedia) {
  window.matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList
}
if (typeof window.ResizeObserver === 'undefined') {
  ;(window as unknown as { ResizeObserver: unknown }).ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}
if (!Element.prototype.scrollTo) {
  Element.prototype.scrollTo = () => {}
}
if (!Element.prototype.getAnimations) {
  Element.prototype.getAnimations = () => []
}
if (!Element.prototype.checkVisibility) {
  // jsdom has no layout: an element shows unless it or an ancestor is hidden or display:none.
  Element.prototype.checkVisibility = function (this: Element) {
    for (let el: Element | null = this; el; el = el.parentElement) {
      if (el.hasAttribute('hidden') || getComputedStyle(el).display === 'none') return false
    }
    return this.isConnected
  }
}
if (!Element.prototype.scrollIntoView) {
  // Kumo's tabs scroll the active tab into view on click.
  Element.prototype.scrollIntoView = () => {}
}
if (typeof window.IntersectionObserver === 'undefined') {
  // The on-this-page (table of contents) nav watches the form sections.
  ;(window as unknown as { IntersectionObserver: unknown }).IntersectionObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
    takeRecords() {
      return []
    }
  }
}

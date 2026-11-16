import { useEffect, useState } from 'react'

/** Reads `data-mode` on <html> so charts can follow the app theme. */
export function useIsDarkMode() {
  const read = () => document.documentElement.dataset.mode === 'dark'
  const [dark, setDark] = useState(read)
  useEffect(() => {
    const observer = new MutationObserver(() => setDark(read()))
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-mode'] })
    return () => observer.disconnect()
  }, [])
  return dark
}

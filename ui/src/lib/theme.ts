import { useEffect, useState } from 'react'

export type ThemePreference = 'light' | 'dark' | 'system'

const STORAGE_KEY = 'hostisan-theme'

function savedPreference(): ThemePreference | null {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved === 'light' || saved === 'dark' || saved === 'system') return saved
  } catch {
    // Storage may be blocked; fall back to the system setting.
  }
  return null
}

function readPreference(): ThemePreference {
  return savedPreference() ?? 'system'
}

/**
 * Applies the account's Hestia theme ("dark" or default) unless the person has picked
 * a theme in this browser, which always wins.
 */
export function applyServerTheme(theme: string) {
  if (savedPreference()) return
  applyMode(theme === 'dark' ? 'dark' : 'system')
}

function applyMode(pref: ThemePreference) {
  const dark =
    pref === 'dark' ||
    (pref === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.dataset.mode = dark ? 'dark' : 'light'
}

export function useTheme() {
  const [preference, setPreferenceState] = useState<ThemePreference>(readPreference)

  // Remember only an explicit choice, so an unset browser can follow the account's theme.
  const setPreference = (pref: ThemePreference) => {
    setPreferenceState(pref)
    try {
      localStorage.setItem(STORAGE_KEY, pref)
    } catch {
      // Ignore: the choice still applies for this visit.
    }
  }

  useEffect(() => {
    if (!savedPreference()) return
    applyMode(preference)
    if (preference !== 'system') return
    const media = window.matchMedia('(prefers-color-scheme: dark)')
    const onChange = () => applyMode('system')
    media.addEventListener('change', onChange)
    return () => media.removeEventListener('change', onChange)
  }, [preference])

  return { preference, setPreference }
}

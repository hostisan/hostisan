/**
 * A language's name in that language ("Deutsch", "Français"), for a language picker
 * that people who can't read the current language still need to use. The code itself
 * when the browser doesn't know it.
 */
export function nativeLanguageName(code: string): string {
  try {
    const name = new Intl.DisplayNames([code], { type: 'language' }).of(code)
    if (!name || name === code) return code
    return name.charAt(0).toLocaleUpperCase(code) + name.slice(1)
  } catch {
    return code
  }
}

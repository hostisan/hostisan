// The same replacements as the server's brand.Rewrite (hostisan/internal/brand), longest
// form first, for English msgids that name Hestia.
const replacements: [RegExp, string][] = [
  [/Hestia Control Panel/g, 'Hostisan'],
  [/HestiaCP/g, 'Hostisan'],
  [/Hestia/g, 'Hostisan'],
]

export function rewriteBrand(s: string): string {
  return replacements.reduce((out, [from, to]) => out.replace(from, to), s)
}

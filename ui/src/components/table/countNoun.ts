/** A list noun: plain (already right) or the singular/plural pair Hestia's ngettext uses. */
export type Noun = string | { one: string; other: string }

/** Picks the form for a count; a plain string is used as is. */
export function countNoun(count: number, noun: Noun): string {
  if (typeof noun === 'string') return noun
  return count === 1 ? noun.one : noun.other
}

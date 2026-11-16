/**
 * Marks a msgid that is stored now and translated later with `t(x)`, such as a label
 * in a constant table. It returns the string unchanged; the extractor and the
 * untranslated-text audit recognise the call.
 */
export function msgid<T extends string>(s: T): T {
  return s
}

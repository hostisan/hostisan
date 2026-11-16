import { describe, expect, it } from 'vitest'
import { auditedFiles, findUntranslatedIn } from './i18nAudit'

function audit(source: string) {
  return findUntranslatedIn({ 'src/fixture.tsx': source }).map(({ kind, text, line }) => ({ kind, text, line }))
}

describe('untranslated text audit', () => {
  it('finds JSX text', () => {
    expect(audit(`export const A = () => (\n  <p>\n    Add domain\n  </p>\n)`)).toEqual([
      { kind: 'jsx-text', text: 'Add domain', line: 3 },
    ])
    expect(audit(`export const A = () => <p>{'Add domain'}</p>`)).toEqual([
      { kind: 'jsx-text', text: 'Add domain', line: 1 },
    ])
    expect(audit(`export const A = () => <p>{t('Add domain')} {name} 42 · {n}</p>`)).toEqual([])
  })

  it('finds a literal label attribute', () => {
    expect(audit(`export const A = () => <Input label="Domain" placeholder={'example.com'} />`)).toEqual([
      { kind: 'jsx-attr', text: 'Domain', line: 1 },
      { kind: 'jsx-attr', text: 'example.com', line: 1 },
    ])
    expect(audit(`export const A = () => <Button aria-label={open ? 'Close' : t('Open')} />`)).toEqual([
      { kind: 'jsx-attr', text: 'Close', line: 1 },
    ])
    // Not a translatable attribute.
    expect(audit(`export const A = () => <a href="/web" className="text-sm" data-shortcut="add" />`)).toEqual([])
  })

  it('finds a literal noun or resource type', () => {
    expect(
      audit(
        `export const A = () => (\n  <>\n    <TableCard noun={{ one: 'rule', other: t('rules') }} />\n    <TableCard noun={n === 1 ? 'key' : 'keys'} />\n    <ResourceDeleteDialog resourceType="web domain" />\n  </>\n)`,
      ),
    ).toEqual([
      { kind: 'jsx-attr', text: 'rule', line: 3 },
      { kind: 'jsx-attr', text: 'key', line: 4 },
      { kind: 'jsx-attr', text: 'keys', line: 4 },
      { kind: 'jsx-attr', text: 'web domain', line: 5 },
    ])
    expect(audit(`export const A = () => <TableCard noun={{ one: t('rule'), other: t('rules') }} resourceType={t('database')} />`)).toEqual([])
  })

  it('finds an untranslated object label', () => {
    expect(audit(`const items = [\n  { label: 'Web domains', value: 'web' },\n  { title: \`Mail\` },\n]`)).toEqual([
      { kind: 'object-key', text: 'Web domains', line: 2 },
      { kind: 'object-key', text: 'Mail', line: 3 },
    ])
    expect(audit(`const x = { value: 'Web domains', id: 'mail' }`)).toEqual([])
  })

  it('accepts msgid()', () => {
    expect(audit(`const items = [{ label: msgid('Web domains') }, { label: t('Mail') }]`)).toEqual([])
  })

  it('flags a template literal in t()', () => {
    expect(audit('const s = t(`Delete ${name}?`)')).toEqual([{ kind: 't-template', text: '`Delete ${name}?`', line: 1 }])
    // No substitution: a plain msgid.
    expect(audit('const s = t(`Delete the domain?`)')).toEqual([])
  })

  it('flags concatenation in t()', () => {
    expect(audit(`const s = t('Delete ' + name)`)).toEqual([{ kind: 't-concat', text: "'Delete ' + name", line: 1 }])
  })

  it('honours the allow list', () => {
    expect(
      audit(`export const A = () => <><p>DKIM</p><p>phpMyAdmin</p><Badge label="SSL" /><p> — </p><p>/</p></>`),
    ).toEqual([])
    expect(audit(`const o = { label: 'IPv6' }`)).toEqual([])
  })

  it('reads real source folders and skips tests', () => {
    const files = auditedFiles(['src/designs'])
    expect(files).toContain('src/designs/dns/DnsZonesPage.tsx')
    expect(files.every((f) => f.startsWith('src/designs/') && !f.includes('.test.'))).toBe(true)
    expect(auditedFiles(['designs/dns']).every((f) => f.startsWith('src/designs/dns/'))).toBe(true)
    // The whole tree, spelt either way, holds every source file.
    for (const all of [auditedFiles(['src']), auditedFiles(['src/']), auditedFiles(['./src'])]) {
      expect(all).toContain('src/App.tsx')
      expect(all).toContain('src/designs/dns/DnsZonesPage.tsx')
      expect(all.some((f) => f.startsWith('src/test/') || f.endsWith('.gen.ts'))).toBe(false)
    }
  })
})

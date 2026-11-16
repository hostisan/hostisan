import { describe, expect, it } from 'vitest'
import { isMailFeatureOn, mailHostname, mailWebmailUrl } from './shared'

describe('mail shared helpers', () => {
  it('builds webmail links from the system alias (Parity 7)', () => {
    expect(mailWebmailUrl('webmail', 'example.com')).toBe('https://webmail.example.com/')
    expect(mailWebmailUrl('mail', 'example.com', 'info')).toBe('https://mail.example.com/?_user=info@example.com')
    expect(mailWebmailUrl('', 'example.com')).toBe('https://webmail.example.com/')
  })
  it('treats anything but "no" as on, as the PHP lists did', () => {
    expect(isMailFeatureOn('yes')).toBe(true)
    expect(isMailFeatureOn('')).toBe(true)
    expect(isMailFeatureOn('no')).toBe(false)
  })
  it('names the mail host', () => {
    expect(mailHostname('example.com')).toBe('mail.example.com')
  })
})

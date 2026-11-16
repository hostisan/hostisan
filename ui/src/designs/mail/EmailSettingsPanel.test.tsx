import { TooltipProvider } from '@cloudflare/kumo'
import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { EmailSettingsPanel } from './EmailSettingsPanel'

afterEach(cleanup)

const show = (props: Partial<Parameters<typeof EmailSettingsPanel>[0]> = {}) =>
  render(
    <TooltipProvider>
      <EmailSettingsPanel account="info" domain="example.com" password="Secret123" {...props} />
    </TooltipProvider>,
  )

describe('email settings panel (checklist 1.8)', () => {
  it('shows the username, password and mail host', () => {
    show()
    expect(screen.getByText('info@example.com')).toBeTruthy()
    expect(screen.getByText('Secret123')).toBeTruthy()
    expect(screen.getByText('mail.example.com')).toBeTruthy()
  })

  it('mirrors a typed account name and a placeholder before one is typed', () => {
    show({ account: '', password: '' })
    expect(screen.getByText('account@example.com')).toBeTruthy()
    expect(screen.getByText('–')).toBeTruthy()
  })

  it('shows the webmail address only when webmail is on', () => {
    show({ webmailAlias: 'webmail' })
    expect(screen.getByText('https://webmail.example.com/')).toBeTruthy()
    cleanup()
    show()
    expect(screen.queryByText('Webmail')).toBeNull()
  })

  it('lists the IMAP, POP3 and SMTP ports exactly', () => {
    show()
    const row = (name: string) => within(screen.getByText(`${name} settings`).closest('tr')!)
    const ports = (name: string) =>
      row(name)
        .getAllByText(/^Port /)
        .map((c) => c.textContent)
    expect(ports('IMAP')).toEqual(['Port 993', 'Port 143', 'Port 143'])
    expect(ports('POP3')).toEqual(['Port 995', 'Port 110', 'Port 110'])
    expect(ports('SMTP')).toEqual(['Port 465', 'Port 587', 'Port 25'])
    expect(screen.getAllByText('Normal password')).toHaveLength(3)
  })
})

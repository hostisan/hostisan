import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { mailRoutes } from '../../test/mailFixtures'
import { renderApp, type Route } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const fr: Record<string, string> = {
  'Changes have been saved.': 'Les modifications ont été enregistrées.',
  Mail: 'Messagerie',
  'Mail domains': 'Domaines de messagerie',
  'Mail domain': 'Domaine de messagerie',
  Accounts: 'Comptes',
  Status: 'Statut',
  'Rate limit': "Limite d'envoi",
  'Catch-all email': 'E-mail fourre-tout',
  'Actions for %s': 'Actions pour %s',
  Save: 'Enregistrer',
}

/** The mail fixtures, signed in with French as the session language. */
function french(over: Record<string, Route> = {}) {
  const routes = mailRoutes({}, over)
  const boot = routes['GET /bootstrap']
  return {
    ...routes,
    'GET /bootstrap': (b: unknown) => {
      const reply = boot(b)
      return { ...reply, body: { ...(reply.body as object), language: 'fr' } }
    },
    'GET /i18n/fr?v=1.0.0': () => ({ body: fr }),
  }
}

describe('mail screens in French', () => {
  it('Mail domains page shows French column headers', async () => {
    renderApp('/mail', french())
    expect(await screen.findByRole('heading', { name: 'Messagerie' })).toBeTruthy()
    expect(await screen.findByRole('columnheader', { name: /Comptes/ })).toBeTruthy()
    expect(screen.getByRole('columnheader', { name: /Statut/ })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Actions pour example.com' })).toBeTruthy()
  })

  it("MailDomainForm toast uses Hestia's saved msgid", async () => {
    const app = renderApp('/mail/example.com/edit', french({ 'PUT /mail/example.com': () => ({ body: { status: 'ok' } }) }))
    expect(await screen.findByLabelText(/^E-mail fourre-tout/)).toBeTruthy()
    expect(screen.getByLabelText("Limite d'envoi")).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /^(Save|Enregistrer)$/ }))
    await waitFor(() => expect(app.location()).toBe('/mail'))
    expect(await screen.findByText('Les modifications ont été enregistrées.')).toBeTruthy()
  })
})

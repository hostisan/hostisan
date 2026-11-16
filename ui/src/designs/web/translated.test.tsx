import { cleanup, fireEvent, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp, signedIn, type Route } from '../../test/renderApp'
import { webRoutes } from '../../test/webFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const fr: Record<string, string> = {
  'Web domains': 'Domaines web',
  Domain: 'Domaine',
  Disk: 'Disque',
  Status: 'Statut',
  'Add web domain': 'Ajouter un domaine web',
  'Actions for %s': 'Actions pour %s',
  Suspend: 'Suspendre',
  'Are you sure you want to suspend domain %s?': 'Voulez-vous vraiment suspendre le domaine %s ?',
  'Edit web domain': 'Modifier le domaine web',
  'Enable domain redirection': 'Activer la redirection du domaine',
}

/** The web routes, signed in with French as the user's language. */
const french = (): Record<string, Route> => ({
  ...webRoutes(),
  'GET /bootstrap': () => ({ body: { ...signedIn({}), language: 'fr' } }),
  'GET /i18n/fr?v=1.0.0': () => ({ body: fr }),
})

describe('web screens in French', () => {
  it('WebDomainsPage shows French column headers', async () => {
    renderApp('/web', french())
    expect(await screen.findByRole('heading', { name: 'Domaines web' })).toBeTruthy()
    const headers = screen.getAllByRole('columnheader').map((h) => h.textContent)
    expect(headers).toEqual(expect.arrayContaining(['Domaine', 'Disque', 'Statut']))
    expect(screen.getAllByRole('link', { name: 'Ajouter un domaine web' }).length).toBeGreaterThan(0)
  })

  it('fills the domain into the translated suspend confirm', async () => {
    renderApp('/web', french())
    fireEvent.click(await screen.findByRole('button', { name: 'Actions pour example.com' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Suspendre' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/Voulez-vous vraiment suspendre le domaine example\.com \?/)).toBeTruthy()
  })

  it('EditWebDomainPage labels follow the language', async () => {
    renderApp('/web/example.com', french())
    expect(await screen.findByRole('heading', { name: 'Modifier le domaine web' })).toBeTruthy()
    expect(screen.getAllByRole('switch', { name: 'Activer la redirection du domaine' }).length).toBeGreaterThan(0)
  })
})

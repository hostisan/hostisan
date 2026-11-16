import { cleanup, fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { dbRoutes, renderApp, signedIn, type Route } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const fr: Record<string, string> = {
  'Add database': 'Ajouter une base de données',
  Username: "Nom d'utilisateur",
  Status: 'Statut',
  'Search databases': 'Rechercher des bases de données',
  'Actions for %s': 'Actions pour %s',
  Suspend: 'Suspendre',
  'Suspend database': 'Suspendre la base de données',
  'Are you sure you want to suspend database %s?': 'Voulez-vous vraiment suspendre la base de données %s ?',
  'Databases (%s suspended)': 'Bases de données (%s suspendues)',
  'Database user': 'Utilisateur de la base',
  'Advanced options': 'Options avancées',
  'It is strongly advised to {create a standard user account} before adding %s to the server due to the increased privileges the admin account possesses and potential security risks.':
    'Il est fortement conseillé de {créer un compte utilisateur standard} avant d’ajouter %s au serveur.',
  'a database': 'une base de données',
}

function french(options: Record<string, unknown> = {}): Record<string, Route> {
  return {
    ...dbRoutes({}, options),
    'GET /bootstrap': () => ({ body: { ...signedIn({}), language: 'fr' } }),
    'GET /i18n/fr?v=1.0.0': () => ({ body: fr }),
  }
}

describe('database pages in French', () => {
  it('databases page shows French headings, search and aside', async () => {
    renderApp('/db', french())
    expect((await screen.findAllByRole('link', { name: 'Ajouter une base de données' })).length).toBeGreaterThan(0)
    expect(screen.getByRole('columnheader', { name: /Nom d'utilisateur/ })).toBeTruthy()
    expect(screen.getByRole('columnheader', { name: 'Statut' })).toBeTruthy()
    expect(screen.getAllByLabelText('Rechercher des bases de données').length).toBeGreaterThan(0)
    expect(screen.getByText('Bases de données (1 suspendues)')).toBeTruthy()
  })

  it('suspend confirm fills the database into Hestia’s sentence', async () => {
    renderApp('/db', french())
    fireEvent.click(await screen.findByRole('button', { name: 'Actions pour admin_wordpress' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Suspendre' }))
    expect(await screen.findByText('Voulez-vous vraiment suspendre la base de données admin_wordpress ?')).toBeTruthy()
    expect(screen.getByText('Suspendre la base de données')).toBeTruthy()
  })

  it('add form labels and the admin warning follow the language', async () => {
    renderApp('/db/new', french({ adminGate: true }))
    expect(
      await screen.findByText(
        'Il est fortement conseillé de créer un compte utilisateur standard avant d’ajouter une base de données au serveur.',
      ),
    ).toBeTruthy()
    cleanup()
    renderApp('/db/new', french())
    expect(await screen.findByText('Utilisateur de la base')).toBeTruthy()
    expect(screen.getByText('Options avancées')).toBeTruthy()
  })
})

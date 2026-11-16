import { cleanup, fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { profileRoutes, usersRoutes } from '../../test/accountFixtures'
import { renderApp, type Route } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const full = {
  web: true, dns: true, db: true, mail: true, cron: true, backup: true, incremental: true, logs: true,
  accessKeys: true, server: true, firewall: true, firewallBans: true,
}

const fr: Record<string, string> = {
  User: 'Utilisateur',
  Package: 'Forfait',
  Status: 'Statut',
  'Add user': 'Ajouter un utilisateur',
  'Actions for %s': 'Actions pour %s',
  'Log in as %s': 'Se connecter en tant que %s',
  Suspend: 'Suspendre',
  'Suspend user': "Suspendre l'utilisateur",
  'Are you sure you want to suspend user %s?': "Voulez-vous vraiment suspendre l'utilisateur %s ?",
  'Package name': 'Nom du forfait',
  'Storage and backups': 'Stockage et sauvegardes',
  'Changes have been saved.': 'Les modifications ont été enregistrées.',
  Save: 'Enregistrer',
  'Contact name': 'Nom du contact',
  'Password and login': 'Mot de passe et connexion',
}

function french(over: Record<string, Route> = {}): Record<string, Route> {
  const routes = { ...usersRoutes({ features: full }), ...profileRoutes({ features: full }, over) }
  const boot = routes['GET /bootstrap']
  return {
    ...routes,
    'GET /bootstrap': (b) => {
      const r = boot(b)
      return { ...r, body: { ...(r.body as object), language: 'fr' } }
    },
    'GET /i18n/fr?v=1.0.0': () => ({ body: fr }),
  }
}

describe('users pages in French', () => {
  it('UsersPage shows French column headers and add button', async () => {
    renderApp('/users', french())
    expect(await screen.findByRole('link', { name: 'Ajouter un utilisateur' })).toBeTruthy()
    await screen.findByRole('table')
    expect(screen.getByRole('columnheader', { name: /Utilisateur/ })).toBeTruthy()
    expect(screen.getByRole('columnheader', { name: 'Statut' })).toBeTruthy()
  })

  it('user suspend confirm fills the name into Hestia’s sentence', async () => {
    renderApp('/users', french())
    fireEvent.click(await screen.findByRole('button', { name: 'Actions pour bob' }))
    expect(await screen.findByRole('menuitem', { name: 'Se connecter en tant que bob' })).toBeTruthy()
    fireEvent.click(screen.getByRole('menuitem', { name: 'Suspendre' }))
    expect(await screen.findByText("Voulez-vous vraiment suspendre l'utilisateur bob ?")).toBeTruthy()
    expect(screen.getByText("Suspendre l'utilisateur")).toBeTruthy()
  })

  it('UserForm labels follow the language', async () => {
    renderApp('/users/bob', french())
    expect((await screen.findAllByText('Nom du contact')).length).toBeGreaterThan(0)
    expect(screen.getAllByText('Mot de passe et connexion').length).toBeGreaterThan(0)
  })

  it('PackageForm labels and save toast use translated msgids', async () => {
    renderApp(
      '/packages/starter-plus',
      french({ 'PUT /packages/starter-plus': () => ({ body: { status: 'ok', name: 'starter-plus' } }) }),
    )
    expect(await screen.findByLabelText('Nom du forfait')).toBeTruthy()
    expect(screen.getAllByText('Stockage et sauvegardes').length).toBeGreaterThan(0)
    fireEvent.click(screen.getByRole('button', { name: 'Enregistrer' }))
    expect(await screen.findByText('Les modifications ont été enregistrées.')).toBeTruthy()
  })
})

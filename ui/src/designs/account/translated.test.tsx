import { cleanup, fireEvent, screen, within } from '@testing-library/react'
import type { ComponentProps } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { keysRoutes, statsRoutes } from '../../test/accountFixtures'
import { renderApp, signedIn, type Route } from '../../test/renderApp'

// jsdom has no canvas: the chart becomes a plain list, as in StatsPage.test.tsx.
vi.mock('@cloudflare/kumo', async (orig) => ({
  ...(await orig<typeof import('@cloudflare/kumo')>()),
  TimeseriesChart: ({ ariaDescription }: ComponentProps<typeof import('@cloudflare/kumo').TimeseriesChart>) => (
    <ol aria-label={ariaDescription} />
  ),
}))

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const fr: Record<string, string> = {
  'Access keys': "Clés d'accès",
  'Add access key': "Ajouter une clé d'accès",
  'Access key': "Clé d'accès",
  Comment: 'Commentaire',
  Permissions: 'Autorisations',
  'Actions for %s': 'Actions pour %s',
  Delete: 'Supprimer',
  'Delete access key': "Supprimer la clé d'accès",
  'Are you sure you want to delete access key %s?': "Voulez-vous vraiment supprimer la clé d'accès %s ?",
  'SSH keys': 'Clés SSH',
  'Add SSH key': 'Ajouter une clé SSH',
  'SSH ID': 'ID SSH',
  Fingerprint: 'Empreinte',
  Logs: 'Journaux',
  'Login history': 'Historique des connexions',
  Category: 'Catégorie',
  Message: 'Message',
  Statistics: 'Statistiques',
  Oct: 'oct.',
  'Monthly breakdown': 'Détail mensuel',
}

function french(routes: Record<string, Route>): Record<string, Route> {
  return {
    ...routes,
    'GET /bootstrap': () => ({ body: { ...signedIn({}), language: 'fr' } }),
    'GET /i18n/fr?v=1.0.0': () => ({ body: fr }),
  }
}

describe('Account pages in French', () => {
  it('access keys page shows French headings', async () => {
    renderApp('/account/access-keys', french(keysRoutes()))
    expect(await screen.findByRole('heading', { name: "Clés d'accès" })).toBeTruthy()
    expect(screen.getAllByRole('link', { name: "Ajouter une clé d'accès" }).length).toBeGreaterThan(0)
    expect(screen.getByRole('columnheader', { name: /Commentaire/ })).toBeTruthy()
    expect(screen.getByRole('columnheader', { name: 'Autorisations' })).toBeTruthy()
  })

  it('access key delete confirm uses Hestia’s msgid with the key', async () => {
    renderApp('/account/access-keys', french(keysRoutes()))
    fireEvent.click(await screen.findByRole('button', { name: 'Actions pour aB3dEfGh1JkLmN0pQrSt' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Supprimer' }))
    const dialog = await screen.findByRole('dialog', { name: "Supprimer la clé d'accès" })
    expect(within(dialog).getByText(/Voulez-vous vraiment supprimer la clé d'accès aB3dEfGh1JkLmN0pQrSt \?/)).toBeTruthy()
  })

  it('SSH keys page labels follow the language', async () => {
    renderApp('/account/ssh-keys', french(keysRoutes()))
    expect(await screen.findByRole('heading', { name: 'Clés SSH' })).toBeTruthy()
    expect(screen.getByRole('columnheader', { name: 'ID SSH' })).toBeTruthy()
    expect(screen.getByRole('columnheader', { name: 'Empreinte' })).toBeTruthy()
  })

  it('logs page headings follow the language', async () => {
    renderApp('/logs', french(keysRoutes()))
    expect(await screen.findByRole('heading', { name: 'Journaux' })).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Historique des connexions' })).toBeTruthy()
    expect(screen.getByRole('columnheader', { name: /Catégorie/ })).toBeTruthy()
  })

  it('stats months use Hestia’s translated month names', async () => {
    renderApp('/stats', french(statsRoutes()))
    expect(await screen.findByRole('heading', { name: 'Statistiques' })).toBeTruthy()
    expect(await screen.findByText('Détail mensuel')).toBeTruthy()
    expect(screen.getByText('oct. 2026')).toBeTruthy()
  })
})

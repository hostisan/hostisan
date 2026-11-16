import { cleanup, fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { backupRoutes } from '../../test/accountFixtures'
import { renderApp, signedIn, type Route } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const NEW = 'admin.2026-10-20_03-00-01.tar'
const OLD = 'admin.2026-10-19_03-00-02.tar'

const fr: Record<string, string> = {
  Oct: 'oct.',
  'File name': 'Nom du fichier',
  Runtime: 'Durée',
  'Backup exclusions': 'Exclusions de sauvegarde',
  '%d hour': '%d heure',
  'Actions for %s': 'Actions pour %s',
  Delete: 'Supprimer',
  'Are you sure you want to delete backup %s?': 'Voulez-vous vraiment supprimer la sauvegarde %s ?',
  'Edit backup exclusions': 'Modifier les exclusions de sauvegarde',
  'Type full database name, one per line. To exclude all databases use *':
    'Saisissez le nom complet de la base, un par ligne. Pour exclure toutes les bases, utilisez *',
  'Create snapshot': 'Créer un instantané',
  Symlink: 'Lien symbolique',
}

function french(): Record<string, Route> {
  return {
    ...backupRoutes(),
    'GET /bootstrap': () => ({ body: { ...signedIn({}), language: 'fr' } }),
    'GET /i18n/fr?v=1.0.0': () => ({ body: fr }),
  }
}

describe('backup pages in French', () => {
  it('Backups list date uses translated month', async () => {
    renderApp('/backup', french())
    expect(await screen.findByRole('link', { name: NEW })).toBeTruthy()
    expect((await screen.findAllByText('20 oct. 2026')).length).toBeGreaterThan(0)
  })

  it('Backups list shows French headers, tabs and runtimes', async () => {
    renderApp('/backup', french())
    expect(await screen.findByRole('columnheader', { name: /Nom du fichier/ })).toBeTruthy()
    expect(screen.getByRole('columnheader', { name: /Durée/ })).toBeTruthy()
    expect(screen.getByRole('tab', { name: 'Exclusions de sauvegarde' })).toBeTruthy()
    expect(screen.getByText('1 heure')).toBeTruthy()
  })

  it('delete confirm fills the backup name into Hestia’s sentence', async () => {
    renderApp('/backup', french())
    fireEvent.click(await screen.findByRole('button', { name: `Actions pour ${OLD}` }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Supprimer' }))
    expect(await screen.findByText(`Voulez-vous vraiment supprimer la sauvegarde ${OLD} ?`)).toBeTruthy()
  })

  it('exclusions form uses Hestia’s heading and placeholder', async () => {
    renderApp('/backup/exclusions/edit', french())
    expect(await screen.findByRole('heading', { name: 'Modifier les exclusions de sauvegarde' })).toBeTruthy()
    expect(
      screen.getByPlaceholderText(
        'Saisissez le nom complet de la base, un par ligne. Pour exclure toutes les bases, utilisez *',
      ),
    ).toBeTruthy()
  })

  it('snapshot pages follow the language', async () => {
    renderApp('/backup/incremental', french())
    expect((await screen.findAllByRole('button', { name: /Créer un instantané/ })).length).toBeGreaterThan(0)
  })
})

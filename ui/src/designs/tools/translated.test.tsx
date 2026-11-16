import { cleanup, fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { keysRoutes } from '../../test/accountFixtures'
import { renderApp, signedIn } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const fr: Record<string, string> = {
  'Mark all as read': 'Tout marquer comme lu',
  'Delete all': 'Tout supprimer',
  Notification: 'Notification',
  Received: 'Reçue',
  'All (%s)': 'Toutes (%s)',
  'Unread (%s)': 'Non lues (%s)',
  'Delete all notifications?': 'Supprimer toutes les notifications ?',
  'All %s notifications will be removed, including unread ones. This cannot be undone.':
    'Les %s notifications seront supprimées, y compris les non lues. Action irréversible.',
  'Web Domain': 'Domaine web',
  Type: 'Type FR',
}

const french = (overrides = {}) =>
  keysRoutes(
    {},
    {
      'GET /bootstrap': () => ({ body: { ...signedIn({}), language: 'fr' } }),
      'GET /i18n/fr?v=1.0.0': () => ({ body: fr }),
      ...overrides,
    },
  )

describe('tools pages in French', () => {
  it('notifications page shows French actions, tabs and headers', async () => {
    renderApp('/notifications', french())
    expect(await screen.findByRole('button', { name: 'Tout marquer comme lu' })).toBeTruthy()
    expect(screen.getByRole('columnheader', { name: /Reçue/ })).toBeTruthy()
    expect(screen.getByRole('tab', { name: /^Toutes \(\d+\)$/ })).toBeTruthy()
    expect(screen.getByRole('tab', { name: /^Non lues \(\d+\)$/ })).toBeTruthy()
  })

  it('delete-all confirm fills the count into a translated sentence', async () => {
    renderApp('/notifications', french())
    fireEvent.click(await screen.findByRole('button', { name: 'Tout supprimer' }))
    expect(await screen.findByText('Supprimer toutes les notifications ?')).toBeTruthy()
    expect(screen.getByText(/^Les \d+ notifications seront supprimées/)).toBeTruthy()
  })

  it('search results name the object type in French', async () => {
    renderApp(
      '/search?q=example',
      french({
        'GET /search?q=example': () => ({
          body: {
            rows: [
              {
                id: '1', type: 'web', key: 'DOMAIN', result: 'example.com', alias: '', user: 'admin', date: '2026-11-01',
                time: '10:00:00', suspended: false, linked: true, route: '/web/example.com', loginAs: false,
              },
            ],
          },
        }),
      }),
    )
    expect(await screen.findByText('Domaine web')).toBeTruthy()
  })
})

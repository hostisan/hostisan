import { cleanup, fireEvent, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { keysRoutes } from '../test/accountFixtures'
import { renderApp, signedIn } from '../test/renderApp'
import { webCommands } from './commands/web'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const fr: Record<string, string> = {
  'Server status': 'État du serveur',
  'Privacy policy': 'Politique de confidentialité',
  Notifications: 'Notifications FR',
  'Notifications, unread': 'Notifications non lues',
  'View all notifications': 'Voir toutes les notifications',
  New: 'Nouveau',
  'Add web domain': 'Ajouter un domaine web',
  'Web domains': 'Domaines web',
}

const french = (user = {}) =>
  keysRoutes(user, {
    'GET /bootstrap': () => ({ body: { ...signedIn(user), language: 'fr' } }),
    'GET /i18n/fr?v=1.0.0': () => ({ body: fr }),
  })

describe('shell pieces in French', () => {
  it('footer links follow the language', async () => {
    renderApp('/error', french())
    expect(await screen.findByRole('link', { name: 'État du serveur' })).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Politique de confidentialité' })).toBeTruthy()
  })

  it('the notifications tray speaks French', async () => {
    renderApp('/error', french({ notifications: true }))
    fireEvent.click(await screen.findByRole('button', { name: 'Notifications non lues' }))
    const tray = await screen.findByRole('dialog')
    expect(await within(tray).findByText('Voir toutes les notifications')).toBeTruthy()
    expect(within(tray).getByText('Notifications FR')).toBeTruthy()
  })

  it('palette entries are translated by the section', () => {
    const t = (s: string) => fr[s] ?? s
    const got = webCommands(true, false, undefined, t)
    expect(got.actions.map((a) => a.title)).toEqual(['Ajouter un domaine web'])
    expect(got.groups.map((g) => g.label)).toEqual(['Domaines web'])
  })
})

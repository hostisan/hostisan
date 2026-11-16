import { cleanup, fireEvent, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp, signedIn, type Route } from '../../test/renderApp'
import { JOB_ID, jobRunning, serverRoutes } from '../../test/serverFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const fr: Record<string, string> = {
  Firewall: 'Pare-feu',
  'Actions for rule %s': 'Actions pour la règle %s',
  'Are you sure you want to delete rule #%s?': 'Voulez-vous vraiment supprimer la règle #%s ?',
  'Delete rule?': 'Supprimer la règle ?',
  'Are you sure you want to delete IP address %s?': "Voulez-vous vraiment supprimer l'adresse IP %s ?",
  'Save %s': 'Enregistrer %s',
  'Check %s': 'Vérifier %s',
  'Save %s configuration': 'Enregistrer la configuration de %s',
  'Restart server': 'Redémarrer le serveur',
}

function french(over: Record<string, Route> = {}): Record<string, Route> {
  return serverRoutes({
    'GET /bootstrap': () => ({ body: { ...signedIn({}), language: 'fr' } }),
    'GET /i18n/fr?v=1.0.0': () => ({ body: fr }),
    ...over,
  })
}

describe('server pages in French', () => {
  it('firewall delete confirm fills the rule id into a translated sentence', async () => {
    renderApp('/server/firewall', french())
    expect(await screen.findByRole('heading', { name: 'Pare-feu' })).toBeTruthy()
    fireEvent.click(await screen.findByRole('button', { name: 'Actions pour la règle 5' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Voulez-vous vraiment supprimer la règle #5 ?')).toBeTruthy()
    expect(within(dialog).getByText('Supprimer la règle ?')).toBeTruthy()
  })

  it('IP delete confirm uses Hestia’s msgid with the address', async () => {
    renderApp('/server/ip/10.0.0.5', french())
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText("Voulez-vous vraiment supprimer l'adresse IP 10.0.0.5 ?")).toBeTruthy()
  })

  it('service step "Check nginx" is translated with its arg', async () => {
    renderApp(
      '/server/services/nginx',
      french({
        'PUT /server/editors/nginx': () => ({ status: 202, body: { jobId: JOB_ID } }),
        [`GET /server/jobs/${JOB_ID}`]: () => ({
          body: { ...jobRunning('Check nginx', ['Save nginx', 'Check nginx']), title: 'Save nginx configuration' },
        }),
      }),
    )
    fireEvent.change(await screen.findByLabelText('worker_connections'), { target: { value: '9' } })
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }))
    expect(await screen.findByRole('heading', { name: 'Enregistrer la configuration de nginx' })).toBeTruthy()
    expect(screen.getAllByText('Enregistrer nginx').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Vérifier nginx').length).toBeGreaterThan(0)
  })

  it('banlist heading follows the language', async () => {
    renderApp('/server/firewall/banlist', french())
    expect(await screen.findByRole('heading', { name: 'Pare-feu' })).toBeTruthy()
  })
})

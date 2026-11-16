import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cronJob, cronRoutes } from '../../test/accountFixtures'
import { renderApp, signedIn, type Route } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const fr: Record<string, string> = {
  'Add cron job': 'Ajouter une tâche cron',
  Command: 'Commande',
  Schedule: 'Planification',
  'Actions for job %s': 'Actions pour la tâche %s',
  Delete: 'Supprimer',
  'Are you sure you want to delete this cron job?': 'Voulez-vous vraiment supprimer cette tâche cron ?',
  'Every %s minutes': 'Toutes les %s minutes',
  'Every weekday at %s': 'Chaque jour ouvré à %s',
  '%s at %s': '%s à %s',
  'on the %s': 'le %s',
  '%sth': '%s',
  'in %s': 'en %s',
  Jan: 'janv.',
  Jun: 'juin',
  Oct: 'oct.',
  'System time': 'Heure du système',
  Monthly: 'Mensuel',
  'Run command': 'Exécuter la commande',
  'Every month': 'Chaque mois',
}

function french(overrides: Record<string, Route> = {}): Record<string, Route> {
  return {
    ...cronRoutes({}, overrides),
    'GET /bootstrap': () => ({ body: { ...signedIn({}), language: 'fr' } }),
    'GET /i18n/fr?v=1.0.0': () => ({ body: fr }),
  }
}

describe('cron pages in French', () => {
  it('cron list shows French headings and schedules', async () => {
    const jobs = [cronJob('1'), cronJob('2', { MIN: '30', HOUR: '3', WDAY: '1,2,3,4,5' })]
    const jobsList = { jobs, reports: true, usage: { jobs: '2', limit: 'unlimited', suspended: '0' } }
    renderApp('/cron', french({ 'GET /cron': () => ({ body: jobsList }) }))
    expect((await screen.findAllByRole('link', { name: 'Ajouter une tâche cron' })).length).toBeGreaterThan(0)
    expect(screen.getByRole('columnheader', { name: /Commande/ })).toBeTruthy()
    expect(screen.getByRole('columnheader', { name: 'Planification' })).toBeTruthy()
    expect(await screen.findByText('Toutes les 15 minutes')).toBeTruthy()
    expect(screen.getByText('Chaque jour ouvré à 03:30')).toBeTruthy()
  })

  it('cron delete confirm uses Hestia’s msgid', async () => {
    renderApp('/cron', french())
    fireEvent.click(await screen.findByRole('button', { name: 'Actions pour la tâche 1' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Supprimer' }))
    expect(await screen.findByText('Voulez-vous vraiment supprimer cette tâche cron ?')).toBeTruthy()
  })

  it('CronForm month names are translated', async () => {
    renderApp('/cron/new', french())
    const line = await screen.findByText(/Heure du système/)
    await waitFor(() => expect(line.textContent).toMatch(/21 oct\. 2026, 12:00/))
    fireEvent.click(await screen.findByRole('tab', { name: 'Mensuel' }))
    fireEvent.click(await screen.findByRole('combobox', { name: 'Exécuter la commande' }))
    expect(await screen.findByRole('option', { name: 'janv.' })).toBeTruthy()
    expect(screen.getByRole('option', { name: 'Chaque mois' })).toBeTruthy()
  })

  it('a schedule with month names is described in French', async () => {
    const job = cronJob('1', { MIN: '0', HOUR: '12', DAY: '5', MONTH: '1,6' })
    renderApp('/cron/1', french({ 'GET /cron/1': () => ({ body: job }) }))
    expect(await screen.findByText('Le 5 en janv., juin à 12:00')).toBeTruthy()
  })
})

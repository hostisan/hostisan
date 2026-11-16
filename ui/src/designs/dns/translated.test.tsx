import { cleanup, fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { dnsRoutes, renderApp, signedIn, type Route } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const fr: Record<string, string> = {
  'Add DNS domain': 'Ajouter un domaine DNS',
  Records: 'Enregistrements',
  Status: 'Statut',
  'Search DNS zones': 'Rechercher des zones DNS',
  'Actions for %s': 'Actions pour %s',
  'Actions for record %s': "Actions pour l'enregistrement %s",
  Suspend: 'Suspendre',
  'Suspend DNS domain': 'Suspendre le domaine DNS',
  'Are you sure you want to suspend domain %s?': 'Voulez-vous vraiment suspendre le domaine %s ?',
  'Add DNS record': 'Ajouter un enregistrement DNS',
  'IP or value': 'IP ou valeur',
  'Lower values are tried first.': 'Les valeurs basses sont essayées en premier.',
  'Delete DNS record': "Supprimer l'enregistrement DNS",
  'Are you sure you want to delete record %s?': "Voulez-vous vraiment supprimer l'enregistrement %s ?",
  'This zone has no records': "Cette zone n'a aucun enregistrement",
}

function french(): Record<string, Route> {
  return {
    ...dnsRoutes(),
    'GET /bootstrap': () => ({ body: { ...signedIn({}), language: 'fr' } }),
    'GET /i18n/fr?v=1.0.0': () => ({ body: fr }),
  }
}

describe('DNS pages in French', () => {
  it('DNS zones page shows French headings and actions', async () => {
    renderApp('/dns', french())
    expect(await screen.findByRole('link', { name: 'Ajouter un domaine DNS' })).toBeTruthy()
    expect(screen.getByRole('columnheader', { name: /Enregistrements/ })).toBeTruthy()
    expect(screen.getByRole('columnheader', { name: 'Statut' })).toBeTruthy()
    expect(screen.getAllByLabelText('Rechercher des zones DNS').length).toBeGreaterThan(0)
  })

  it('zone suspend confirm fills the domain into a translated sentence', async () => {
    renderApp('/dns', french())
    fireEvent.click(await screen.findByRole('button', { name: 'Actions pour example.com' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Suspendre' }))
    expect(await screen.findByText('Voulez-vous vraiment suspendre le domaine example.com ?')).toBeTruthy()
    expect(screen.getByText('Suspendre le domaine DNS')).toBeTruthy()
  })

  it('record delete confirm uses Hestia’s msgid with the record name', async () => {
    renderApp('/dns/example.com', french())
    fireEvent.click(await screen.findByRole('button', { name: "Actions pour l'enregistrement www CNAME" }))
    fireEvent.click(await screen.findByRole('menuitem', { name: /Delete|Supprimer/ }))
    expect(await screen.findByText("Voulez-vous vraiment supprimer l'enregistrement www ?")).toBeTruthy()
  })

  it('record form labels follow the language', async () => {
    renderApp('/dns/example.com/records/new', french())
    expect(await screen.findByRole('heading', { name: 'Ajouter un enregistrement DNS' })).toBeTruthy()
    expect(screen.getAllByText('IP ou valeur').length).toBeGreaterThan(0)
  })
})

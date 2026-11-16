import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { dnsRoutes, renderApp, signedIn } from '../test/renderApp'
import source from './ShortcutsDialog.tsx?raw'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

async function onDns(extra = {}) {
  const app = renderApp('/dns', { ...dnsRoutes(), ...extra })
  await screen.findByRole('link', { name: 'example.com' })
  return app
}

const dialog = () => screen.queryByRole('dialog', { name: 'Shortcuts' })

describe('shortcuts dialog', () => {
  it('h and ? toggle the dialog', async () => {
    await onDns()
    expect(dialog()).toBeNull()
    expect(fireEvent.keyDown(document.body, { key: 'h' })).toBe(false)
    await waitFor(() => expect(dialog()).not.toBeNull())
    fireEvent.keyDown(document.body, { key: 'h' })
    await waitFor(() => expect(dialog()).toBeNull())
    fireEvent.keyDown(document.body, { key: '?', shiftKey: true })
    await waitFor(() => expect(dialog()).not.toBeNull())
    // Other shortcuts wait while it is open.
    expect(fireEvent.keyDown(document.body, { key: '1' })).toBe(true)
    fireEvent.keyDown(document.body, { key: '?', shiftKey: true })
    await waitFor(() => expect(dialog()).toBeNull())
  })

  it('h does nothing while typing', async () => {
    await onDns()
    const field = screen.getByRole('searchbox', { name: 'Search' })
    field.focus()
    expect(fireEvent.keyDown(field, { key: 'h' })).toBe(true)
    expect(dialog()).toBeNull()
  })

  it("it lists every shortcut with Hestia's translated labels", async () => {
    const fr: Record<string, string> = {
      Shortcuts: 'Raccourcis',
      'Add new object': 'Ajouter un nouvel objet',
      'Save form': 'Enregistrer le formulaire',
      'Leave form without saving': 'Quitter le formulaire sans enregistrer',
      'Go to web list': 'Aller à la liste WEB',
      'Go to DNS list': 'Aller à la liste DNS',
      'Go to mail list': 'Aller à la liste MAIL',
      'Go to DB list': 'Aller à la liste BD',
      'Go to cron list': 'Aller à la liste CRON',
      'Go to backup list': 'Aller à la liste SAUVEGARDE',
      'Focus on search': 'Aller à la recherche',
      'Display / Hide shortcuts': 'Afficher / masquer les raccourcis',
      'Move backward through top menu': 'Reculer dans le menu',
      'Move forward through top menu': 'Avancer dans le menu',
      'Enter focused element': "Ouvrir l'élément actif",
      'Move up through elements list': 'Monter dans la liste',
      'Move down through elements list': 'Descendre dans la liste',
      'Select all': 'Tout sélectionner',
    }
    await onDns({
      'GET /bootstrap': () => ({ body: { ...signedIn({}), language: 'fr' } }),
      'GET /i18n/fr?v=1.0.0': () => ({ body: fr }),
    })
    await screen.findByRole('link', { name: 'example.com' })
    fireEvent.keyDown(document.body, { key: 'h' })
    const box = await screen.findByRole('dialog', { name: 'Raccourcis' })
    const rows = within(box)
      .getAllByRole('listitem')
      .map((li) => li.textContent)
    const labels = Object.values(fr).filter((l) => l !== 'Raccourcis')
    for (const label of labels)
      expect(
        rows.some((r) => r?.includes(label)),
        label,
      ).toBe(true)
    expect(rows).toHaveLength(labels.length)
    expect(within(box).getAllByText('a', { selector: 'kbd' })).toHaveLength(1)
    expect(within(box).getByText('?', { selector: 'kbd' })).toBeTruthy()
  })

  it('the dialog is always mounted', async () => {
    expect(source).toMatch(/<Dialog\.Root open=\{open\}/)
    expect(source).not.toMatch(/\bopen &&/)
    await onDns()
    for (let i = 0; i < 2; i++) {
      fireEvent.keyDown(document.body, { key: 'h' })
      await waitFor(() => expect(dialog()).not.toBeNull())
      fireEvent.keyDown(document.body, { key: 'Escape' })
      await waitFor(() => expect(dialog()).toBeNull())
    }
  })

  it('user menu and palette open it', async () => {
    await onDns()
    fireEvent.click(screen.getByRole('button', { name: 'admin' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Shortcuts' }))
    await waitFor(() => expect(dialog()).not.toBeNull())
    fireEvent.keyDown(document.body, { key: 'Escape' })
    await waitFor(() => expect(dialog()).toBeNull())

    fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
    const input = await screen.findByPlaceholderText(/Search pages, domains/)
    fireEvent.change(input, { target: { value: 'shortc' } })
    fireEvent.click(await screen.findByRole('option', { name: 'Shortcuts' }))
    await waitFor(() => expect(dialog()).not.toBeNull())
    expect(screen.queryByPlaceholderText(/Search pages, domains/)).toBeNull()
  })

  it('phone width shows one column', async () => {
    await onDns()
    fireEvent.keyDown(document.body, { key: 'h' })
    const box = await screen.findByRole('dialog', { name: 'Shortcuts' })
    const columns = box.querySelector('[data-shortcut-columns]') as HTMLElement
    expect(columns.className.split(' ')).toEqual(expect.arrayContaining(['grid-cols-1', 'sm:grid-cols-2']))
    expect(columns.querySelectorAll(':scope > ul')).toHaveLength(2)
  })
})

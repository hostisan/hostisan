import { cleanup, fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp } from '../../test/renderApp'
import { webRoutes } from '../../test/webFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const card = (name: string) => screen.getByRole('heading', { name }).closest('div[class*="flex-col"]') as HTMLElement

describe('quick install list', () => {
  it('lists the installers from the API with a Setup link to each', async () => {
    renderApp('/web/example.com/apps', webRoutes())
    expect(await screen.findByRole('heading', { name: 'WordPress' })).toBeTruthy()
    expect(screen.getAllByRole('heading', { level: 2 })).toHaveLength(22)
    const setup = card('WordPress').querySelector('a') as HTMLAnchorElement
    expect(setup.getAttribute('href')).toBe('/web/example.com/apps/wordpress')
    expect(card('Joomla').textContent).toContain('Version: 6.1.3')
  })

  it('disables an installer no installed PHP version supports', async () => {
    renderApp('/web/example.com/apps', webRoutes())
    await screen.findByRole('heading', { name: 'Symfony' })
    const symfony = card('Symfony')
    expect(symfony.querySelector('a')).toBeNull()
    const button = Array.from(symfony.querySelectorAll('button')).find((b) => b.textContent === 'Setup')
    expect((button as HTMLButtonElement).disabled).toBe(true)
    expect(symfony.textContent).toContain('No supported PHP version is installed')
  })

  it('searches by name', async () => {
    renderApp('/web/example.com/apps', webRoutes())
    await screen.findByRole('heading', { name: 'WordPress' })
    fireEvent.change(screen.getByRole('textbox', { name: 'Search apps' }), { target: { value: 'word' } })
    expect(screen.getAllByRole('heading', { level: 2 })).toHaveLength(1)
    fireEvent.change(screen.getByRole('textbox', { name: 'Search apps' }), { target: { value: 'zzz' } })
    expect(await screen.findByText('No apps match your search')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Show all apps' }))
    expect(screen.getAllByRole('heading', { level: 2 })).toHaveLength(22)
  })

  it('filters by category tab', async () => {
    renderApp('/web/example.com/apps', webRoutes())
    await screen.findByRole('heading', { name: 'WordPress' })
    fireEvent.click(screen.getByRole('tab', { name: 'E-commerce' }))
    const names = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent)
    expect(names).toEqual(expect.arrayContaining(['OpenCart', 'PrestaShop', 'Shopware']))
    expect(names).not.toContain('WordPress')
    fireEvent.click(screen.getByRole('tab', { name: 'Community' }))
    expect(screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent)).toEqual(
      expect.arrayContaining(['DokuWiki', 'Flarum', 'Castopod']),
    )
  })

  it('shows not found for a missing domain, without loading the edit view', async () => {
    const app = renderApp('/web/missing.example/apps', webRoutes())
    expect(await screen.findByText('Web domain not found')).toBeTruthy()
    expect(app.calls.some((c) => c.key === 'GET /web/missing.example')).toBe(false)
  })

  it('says so when the installer is turned off', async () => {
    renderApp(
      '/web/example.com/apps',
      webRoutes({}, {
        'GET /web/example.com/apps': () => ({
          status: 403,
          body: { error: { code: 'forbidden', message: 'Error: app installer is disabled' } },
        }),
      }),
    )
    expect(await screen.findByText('Quick install is turned off')).toBeTruthy()
  })
})

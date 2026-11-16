import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { dnsRoutes, renderApp } from '../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const focusedName = () => document.activeElement?.textContent

describe('sidebar keys', () => {
  it('arrow right moves focus to the next section and wraps', async () => {
    renderApp('/dns', dnsRoutes({ role: 'user', landing: '/dns' }))
    await screen.findByRole('link', { name: 'example.com' })
    const sections = ['Web', 'DNS', 'Mail', 'Databases', 'Cron jobs', 'Backups', 'Logs', 'Statistics']
    const tail = ['Web terminal', 'File manager']
    expect(fireEvent.keyDown(document.body, { key: 'ArrowRight' })).toBe(false)
    expect(focusedName()).toBe('Web')
    for (const name of [...sections.slice(1), ...tail, 'Web']) {
      fireEvent.keyDown(document.activeElement ?? document.body, { key: 'ArrowRight' })
      expect(focusedName()).toBe(name)
    }
    fireEvent.keyDown(document.activeElement as Element, { key: 'ArrowLeft' })
    expect(focusedName()).toBe('File manager')
  })

  it('arrow left from outside starts at the last section', async () => {
    renderApp('/dns', dnsRoutes({ role: 'user', landing: '/dns' }))
    await screen.findByRole('link', { name: 'example.com' })
    fireEvent.keyDown(document.body, { key: 'ArrowLeft' })
    expect(focusedName()).toBe('File manager')
  })

  it('enter opens the focused section', async () => {
    const app = renderApp('/dns', dnsRoutes({ role: 'user', landing: '/dns' }))
    await screen.findByRole('link', { name: 'example.com' })
    fireEvent.keyDown(document.body, { key: 'ArrowRight' })
    fireEvent.keyDown(document.activeElement as Element, { key: 'ArrowRight' })
    fireEvent.keyDown(document.activeElement as Element, { key: 'ArrowRight' })
    expect(focusedName()).toBe('Mail')
    expect(fireEvent.keyDown(document.activeElement as Element, { key: 'Enter' })).toBe(false)
    await waitFor(() => expect(app.location()).toBe('/mail'))
  })

  it('arrows are left alone while typing', async () => {
    renderApp('/dns', dnsRoutes({ role: 'user', landing: '/dns' }))
    const field = await screen.findByRole('searchbox', { name: 'Search' })
    field.focus()
    expect(fireEvent.keyDown(field, { key: 'ArrowRight' })).toBe(true)
    expect(document.activeElement).toBe(field)
  })
})

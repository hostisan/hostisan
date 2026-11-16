import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { dnsRoutes, renderApp } from '../test/renderApp'
import { breadcrumbsFor, findNavLabel } from './nav'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const every = {
  web: true,
  dns: true,
  db: true,
  mail: true,
  cron: true,
  backup: true,
  incremental: true,
  logs: true,
  accessKeys: true,
  server: true,
  firewall: true,
  firewallBans: true,
  terminal: true,
  fileManager: true,
}

const sidebar = async () => within((await screen.findByText('Hosting')).closest('nav') ?? document.body)

describe('feature-gated navigation', () => {
  it('shows every section when every feature is on, with the Server children', async () => {
    renderApp('/dns', dnsRoutes({ features: every }))
    const nav = await sidebar()
    for (const label of ['Web', 'Cron jobs', 'Backups', 'Logs'])
      expect(nav.getByRole('link', { name: label })).toBeTruthy()
    fireEvent.click(nav.getByRole('button', { name: 'Server' }))
    expect(await nav.findByRole('link', { name: 'Firewall' })).toBeTruthy()
    expect(nav.getByRole('link', { name: 'Preview features' })).toBeTruthy()
  })

  it('hides Firewall without the firewall feature', async () => {
    renderApp('/dns', dnsRoutes({ features: { ...every, firewall: false } }))
    const nav = await sidebar()
    fireEvent.click(nav.getByRole('button', { name: 'Server' }))
    expect(await nav.findByRole('link', { name: 'Services' })).toBeTruthy()
    expect(nav.queryByRole('link', { name: 'Firewall' })).toBeNull()
  })

  it('hides Server without the server feature', async () => {
    renderApp('/dns', dnsRoutes({ features: { ...every, server: false } }))
    const nav = await sidebar()
    expect(nav.getByRole('link', { name: 'Users' })).toBeTruthy()
    expect(nav.queryByRole('button', { name: 'Server' })).toBeNull()
  })

  it('hides Web, Cron jobs, Backups and Logs without their features', async () => {
    renderApp('/dns', dnsRoutes({ features: { ...every, web: false, cron: false, backup: false, logs: false } }))
    const nav = await sidebar()
    expect(nav.getByRole('link', { name: 'DNS' })).toBeTruthy()
    for (const label of ['Web', 'Cron jobs', 'Backups', 'Logs'])
      expect(nav.queryByRole('link', { name: label })).toBeNull()
  })

  it('shows Web terminal and File manager under Tools, each behind its feature', async () => {
    renderApp('/dns', dnsRoutes({ features: every }))
    const nav = await sidebar()
    expect(nav.getByRole('link', { name: 'Web terminal' }).getAttribute('href')).toBe('/terminal')
    expect(nav.getByRole('link', { name: 'File manager' }).getAttribute('href')).toBe('/files')
    cleanup()

    renderApp('/dns', dnsRoutes({ features: { ...every, terminal: false } }))
    const noTerm = await sidebar()
    expect(noTerm.queryByRole('link', { name: 'Web terminal' })).toBeNull()
    expect(noTerm.getByRole('link', { name: 'File manager' })).toBeTruthy()
    cleanup()

    renderApp('/dns', dnsRoutes({ features: { ...every, fileManager: false } }))
    const noFiles = await sidebar()
    expect(noFiles.getByRole('link', { name: 'Web terminal' })).toBeTruthy()
    expect(noFiles.queryByRole('link', { name: 'File manager' })).toBeNull()
  })

  it('still shows the notifications bell', async () => {
    renderApp('/dns', dnsRoutes({ features: every }))
    // The unread count may be unknown until the tray loads, so the label can omit it.
    await waitFor(() => expect(screen.getByRole('button', { name: /^Notifications(, (\d+ )?unread)?$/ })).toBeTruthy())
  })

  it('names the preview features page', () => {
    expect(findNavLabel('/server/preview')).toEqual({ section: 'Server', page: 'Preview features' })
    expect(breadcrumbsFor('/server/preview').at(-1)?.label).toBe('Preview features')
  })

  it('names the empty-package page after its heading, not "Error"', () => {
    expect(breadcrumbsFor('/error')).toEqual([{ label: 'Nothing to manage yet', href: '/error' }])
  })
})

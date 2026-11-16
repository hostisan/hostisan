import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { backupRoutes } from '../../test/accountFixtures'
import { renderApp } from '../../test/renderApp'
import { restoreRequest } from './RestoreTable'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const OLD = 'admin.2026-10-19_03-00-02.tar'
const queuedRestore =
  'Task has been added to the queue. You will receive an email notification when your restore has been completed.'

const restoreRoute = (name = OLD) => ({
  [`POST /backup/${name}/restore`]: () => ({ body: { status: 'queued', message: queuedRestore } }),
})

describe('backup detail', () => {
  it('lists the items in the order web, mail, dns, db, cron, udir', async () => {
    renderApp(`/backup/${OLD}`, backupRoutes())
    await screen.findByText('shop.example.com')
    const rows = screen
      .getAllByRole('row')
      .slice(1)
      .map((r) => r.textContent ?? '')
    const kinds = ['Web domains', 'Mail domains', 'DNS zones', 'Databases', 'Cron jobs', 'User directories']
    const firsts = kinds.map((k) => rows.findIndex((r) => r.includes(k)))
    expect(firsts.every((i) => i >= 0)).toBe(true)
    expect([...firsts].sort((a, b) => a - b)).toEqual(firsts)
    expect(rows.some((r) => r.includes('Jobs'))).toBe(true)
  })

  it('lets the Details names wrap so a narrow screen does not clip them', async () => {
    renderApp(`/backup/${OLD}`, backupRoutes())
    const cell = (await screen.findByText('shop.example.com')).closest('td')!
    expect(cell.className).toContain('whitespace-normal')
    expect(cell.className).toContain('overflow-wrap:anywhere')
  })

  it('keeps the Restore column off the names at 390 px', async () => {
    renderApp(`/backup/${OLD}`, backupRoutes())
    const row = (await screen.findByText('shop.example.com')).closest('tr') as HTMLElement
    // below sm the action column scrolls with the row, so neither it nor its fade covers Details
    const head = screen.getByRole('columnheader', { name: 'Restore' })
    const action = within(row).getByRole('button', { name: 'Restore' }).closest('td')!
    for (const cell of [head, action]) {
      expect(cell.className).toContain('max-sm:static')
      expect(cell.className).toContain('max-sm:before:hidden')
    }
    // the table fits: the button shows only its icon and the type label wraps
    const label = within(row).getByText('Restore')
    expect(label.className).toContain('max-sm:sr-only')
    expect(within(row).getByText('Web domains').closest('td')!.className).toContain('max-sm:whitespace-normal')
  })

  it('asks before restoring one item and sends only that item', async () => {
    const { calls } = renderApp(`/backup/${OLD}`, backupRoutes({}, restoreRoute()))
    await screen.findByText('admin_wp')
    const row = screen.getByText('admin_wp').closest('tr') as HTMLElement
    fireEvent.click(within(row).getByRole('button', { name: 'Restore' }))
    expect(await screen.findByText('Are you sure you want to restore admin_wp?')).toBeTruthy()
    fireEvent.click(screen.getAllByRole('button', { name: 'Restore' }).at(-1) as HTMLElement)
    expect(await screen.findByText(queuedRestore)).toBeTruthy()
    expect(calls.find((c) => c.key === `POST /backup/${OLD}/restore`)?.body).toEqual({
      all: false,
      web: [],
      dns: [],
      mail: [],
      db: ['admin_wp'],
      udir: [],
      cron: false,
    })
  })

  it('uses the cron text for the cron row', async () => {
    const { calls } = renderApp(`/backup/${OLD}`, backupRoutes({}, restoreRoute()))
    await screen.findByText('Jobs')
    fireEvent.click(
      within(screen.getByText('Jobs').closest('tr') as HTMLElement).getByRole('button', { name: 'Restore' }),
    )
    expect(await screen.findByText('Are you sure you want to restore cron jobs?')).toBeTruthy()
    fireEvent.click(screen.getAllByRole('button', { name: 'Restore' }).at(-1) as HTMLElement)
    await waitFor(() => expect(calls.some((c) => c.key === `POST /backup/${OLD}/restore`)).toBe(true))
    expect(calls.find((c) => c.key === `POST /backup/${OLD}/restore`)?.body).toMatchObject({ cron: true, db: [] })
  })

  it('restores everything after confirming', async () => {
    const { calls } = renderApp(`/backup/${OLD}`, backupRoutes({}, restoreRoute()))
    await screen.findByText('admin_wp')
    fireEvent.click(screen.getByRole('button', { name: 'Restore all' }))
    expect(await screen.findByText(/Are you sure you want to restore everything in/)).toBeTruthy()
    fireEvent.click(screen.getAllByRole('button', { name: 'Restore all' }).at(-1) as HTMLElement)
    expect(await screen.findByText(queuedRestore)).toBeTruthy()
    expect(calls.find((c) => c.key === `POST /backup/${OLD}/restore`)?.body).toMatchObject({ all: true })
  })

  it('builds one request from several picked rows', () => {
    expect(
      restoreRequest([
        { type: 'web', object: 'a.com' },
        { type: 'db', object: 'x_1' },
        { type: 'db', object: 'x_2' },
        { type: 'cron', object: 'records' },
        { type: 'udir', object: '.ssh' },
      ]),
    ).toEqual({ all: false, web: ['a.com'], dns: [], mail: [], db: ['x_1', 'x_2'], udir: ['.ssh'], cron: true })
  })

  it('shows not found for an unknown backup', async () => {
    renderApp('/backup/missing', backupRoutes())
    expect(await screen.findByText(/may have been rotated out or deleted/)).toBeTruthy()
  })

  it('hides restore controls when read-only', async () => {
    renderApp(`/backup/${OLD}`, backupRoutes({ readOnly: true }))
    await screen.findByText('admin_wp')
    expect(screen.queryByRole('button', { name: 'Restore all' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Restore' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Download' })).toBeTruthy()
  })
})

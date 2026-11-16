import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { backupRoutes } from '../../test/accountFixtures'
import { renderApp } from '../../test/renderApp'
import type { Translate } from '../../i18n/I18nProvider'
import { formatBackupSize, formatRuntime } from './BackupTabs'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

const NEW = 'admin.2026-10-20_03-00-01.tar'
const OLD = 'admin.2026-10-19_03-00-02.tar'
const queuedBackup =
  'Task has been added to the queue. You will receive an email notification when your backup is ready for download.'

// English: the msgid with its %d filled.
const en: Translate = (id, ...args) => args.reduce<string>((s, a) => s.replace(/%[sd]/, String(a)), id)

describe('backup formats (Hestia 7.9)', () => {
  it('formats sizes, runtimes and dates like Hestia', () => {
    expect(formatBackupSize('312')).toBe('312.00 mb')
    expect(formatBackupSize('1243')).toBe('1.21 gb')
    expect(formatBackupSize('1000')).toBe('0.98 gb')
    expect(formatBackupSize('0')).toBe('0.00 mb')
    expect(formatRuntime('2', en)).toBe('2 minutes')
    expect(formatRuntime('1', en)).toBe('1 minute')
    expect(formatRuntime('75', en)).toBe('1 hour')
    expect(formatRuntime('2880', en)).toBe('2 days')
  })
})

describe('backups list', () => {
  it('lists the backups with Hestia formats', async () => {
    renderApp('/backup', backupRoutes())
    expect(await screen.findByRole('link', { name: NEW })).toBeTruthy()
    expect(screen.getAllByText('20 Oct 2026').length).toBeGreaterThan(0)
    expect(screen.getByText('1.21 gb')).toBeTruthy()
    expect(screen.getByText('1 hour')).toBeTruthy()
    expect(screen.getByText('312.00 mb')).toBeTruthy()
    expect(screen.getByText('2 minutes')).toBeTruthy()
  })

  it('shows a queued toast after Create backup', async () => {
    const { calls } = renderApp(
      '/backup',
      backupRoutes({}, { 'POST /backup': () => ({ body: { status: 'queued', message: queuedBackup } }) }),
    )
    await screen.findByRole('link', { name: NEW })
    fireEvent.click(screen.getAllByRole('button', { name: /Create backup/ })[0])
    expect(await screen.findByText(queuedBackup)).toBeTruthy()
    expect(calls.filter((c) => c.key === 'POST /backup')).toHaveLength(1)
  })

  it('shows the already-running text when the server refuses', async () => {
    const running = 'An existing backup task is already running, please wait for it to complete.'
    renderApp(
      '/backup',
      backupRoutes(
        {},
        { 'POST /backup': () => ({ status: 409, body: { error: { code: 'exists', message: running } } }) },
      ),
    )
    await screen.findByRole('link', { name: NEW })
    fireEvent.click(screen.getAllByRole('button', { name: /Create backup/ })[0])
    expect(await screen.findByText(running)).toBeTruthy()
  })

  it('downloads a local archive through /api/v1/backup/file/<name>', async () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    renderApp(
      '/backup',
      backupRoutes({}, { [`POST /backup/${OLD}/download`]: () => ({ body: { status: 'ready', message: '' } }) }),
    )
    await screen.findByRole('link', { name: OLD })
    fireEvent.click(screen.getByRole('button', { name: `Download ${OLD}` }))
    await waitFor(() => expect(open).toHaveBeenCalledWith(`/api/v1/backup/file/${OLD}`, '_self'))
  })

  it('toasts a scheduled remote download, then refreshes on focus without polling', async () => {
    const msg = 'Download of remote backup file has been scheduled.'
    const open = vi.spyOn(window, 'open').mockImplementation(() => null)
    const { calls } = renderApp(
      '/backup',
      backupRoutes({}, { [`POST /backup/${NEW}/download`]: () => ({ body: { status: 'scheduled', message: msg } }) }),
    )
    await screen.findByRole('link', { name: NEW })
    fireEvent.click(screen.getByRole('button', { name: `Download ${NEW}` }))
    expect(await screen.findByText(msg)).toBeTruthy()
    expect(open).not.toHaveBeenCalled()
    const gets = () => calls.filter((c) => c.key === 'GET /backup').length
    await waitFor(() => expect(gets()).toBeGreaterThanOrEqual(2))
    const before = gets()
    await new Promise((r) => setTimeout(r, 200))
    expect(gets()).toBe(before)
    document.dispatchEvent(new Event('visibilitychange'))
    await new Promise((r) => setTimeout(r, 100))
    expect(gets()).toBe(before + 1) // one refetch on return
    await waitFor(() => expect(gets()).toBe(before + 1))
  })

  it('deletes after confirming', async () => {
    const { calls } = renderApp(
      '/backup',
      backupRoutes({}, { [`DELETE /backup/${OLD}`]: () => ({ body: { status: 'ok' } }) }),
    )
    await screen.findByRole('link', { name: OLD })
    fireEvent.click(screen.getByRole('button', { name: `Actions for ${OLD}` }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    expect(await screen.findByText(`Are you sure you want to delete backup ${OLD}?`)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(calls.some((c) => c.key === `DELETE /backup/${OLD}`)).toBe(true))
  })

  it('hides the incremental tab without the feature', async () => {
    renderApp('/backup', backupRoutes({ features: { backup: true, incremental: false } }))
    await screen.findByRole('link', { name: NEW })
    expect(screen.getByRole('tab', { name: 'Backup exclusions' })).toBeTruthy()
    expect(screen.queryByRole('tab', { name: 'Incremental backups' })).toBeNull()
    cleanup()
    renderApp('/backup', backupRoutes())
    await screen.findByRole('link', { name: NEW })
    expect(screen.getByRole('tab', { name: 'Incremental backups' })).toBeTruthy()
  })

  it('keeps only downloads when read-only', async () => {
    renderApp('/backup', backupRoutes({ readOnly: true }))
    await screen.findByText(NEW)
    expect(screen.queryByRole('button', { name: /Create backup/ })).toBeNull()
    expect(screen.queryByRole('link', { name: NEW })).toBeNull()
    expect(screen.getByRole('button', { name: `Download ${NEW}` })).toBeTruthy()
  })

  it('shows an empty state when there are no backups', async () => {
    renderApp(
      '/backup',
      backupRoutes(
        {},
        { 'GET /backup': () => ({ body: { backups: [], usage: { backups: '0', limit: '10' }, incremental: true } }) },
      ),
    )
    expect(await screen.findByText('No backups yet')).toBeTruthy()
  })

  it('says how many backups are kept only when the limit is known', async () => {
    renderApp(
      '/backup',
      backupRoutes(
        {},
        {
          'GET /backup': () => ({
            body: { backups: [], usage: { backups: '0', limit: 'unlimited' }, incremental: true },
          }),
        },
      ),
    )
    await screen.findByText('No backups yet')
    expect(screen.queryByText(/Keeps the latest/)).toBeNull()
    expect(screen.queryByText(/run nightly/)).toBeNull()
    cleanup()
    renderApp('/backup', backupRoutes())
    expect(await screen.findByText(/Keeps the latest 10 backups/)).toBeTruthy()
  })
})

import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { backupRoutes } from '../../test/accountFixtures'
import { renderApp } from '../../test/renderApp'
import { RESTORE_JOB_ID, restoreJobDone, restoreJobFailed, toolsRoutes } from '../../test/toolsFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const queuedRestore =
  'Task has been added to the queue. You will receive an email notification when your restore has been completed.'

describe('snapshots', () => {
  it('lists snapshots newest first', async () => {
    renderApp('/backup/incremental', backupRoutes())
    await screen.findByRole('link', { name: 'e41f0a29' })
    const rows = screen.getAllByRole('row').map((r) => r.textContent ?? '')
    expect(rows.findIndex((r) => r.includes('e41f0a29'))).toBeLessThan(rows.findIndex((r) => r.includes('3c9b8a7e')))
    expect(screen.getByText('2026-10-20 03:00:05')).toBeTruthy()
    expect(screen.getAllByText('Restic').length).toBeGreaterThan(0)
  })

  it('creates a snapshot and toasts the queued message', async () => {
    const { calls } = renderApp(
      '/backup/incremental',
      backupRoutes(
        {},
        { 'POST /backup/incremental': () => ({ body: { status: 'queued', message: 'Snapshot has been sheduled' } }) },
      ),
    )
    await screen.findByRole('link', { name: 'e41f0a29' })
    fireEvent.click(screen.getByRole('button', { name: /Create snapshot/ }))
    expect(await screen.findByText('Snapshot has been sheduled')).toBeTruthy()
    expect(calls.filter((c) => c.key === 'POST /backup/incremental')).toHaveLength(1)
  })

  it('deletes a snapshot through the bulk route', async () => {
    const { calls } = renderApp(
      '/backup/incremental',
      backupRoutes({}, { 'POST /backup/incremental/bulk': () => ({ body: { status: 'ok', failed: [] } }) }),
    )
    await screen.findByRole('link', { name: '3c9b8a7e' })
    fireEvent.click(screen.getByRole('button', { name: 'Actions for snapshot 3c9b8a7e' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    fireEvent.click(await screen.findByRole('button', { name: 'Delete' }))
    await waitFor(() =>
      expect(calls.find((c) => c.key === 'POST /backup/incremental/bulk')?.body).toEqual({
        action: 'delete',
        names: ['3c9b8a7e'],
      }),
    )
  })

  it('shows an empty list without a repository', async () => {
    renderApp(
      '/backup/incremental',
      backupRoutes({}, { 'GET /backup/incremental': () => ({ body: { snapshots: [] } }) }),
    )
    expect(await screen.findByText('No snapshots yet')).toBeTruthy()
  })
})

describe('snapshot detail', () => {
  it('lists web, mail, dns, db, cron and no user directories, and restores one item', async () => {
    const { calls } = renderApp(
      '/backup/incremental/3c9b8a7e',
      backupRoutes(
        {},
        { 'POST /backup/incremental/3c9b8a7e/restore': () => ({ body: { status: 'queued', message: queuedRestore } }) },
      ),
    )
    await screen.findByText('admin_wp')
    const text = screen.getAllByRole('row').map((r) => r.textContent ?? '')
    expect(text.some((r) => r.includes('User directories'))).toBe(false)
    const row = screen.getByText('admin_wp').closest('tr') as HTMLElement
    fireEvent.click(within(row).getByRole('button', { name: 'Restore' }))
    fireEvent.click(screen.getAllByRole('button', { name: 'Restore' }).at(-1) as HTMLElement)
    expect(await screen.findByText(queuedRestore)).toBeTruthy()
    expect(calls.find((c) => c.key === 'POST /backup/incremental/3c9b8a7e/restore')?.body).toMatchObject({
      all: false,
      db: ['admin_wp'],
    })
  })

  it('shows not found for an unknown snapshot', async () => {
    renderApp('/backup/incremental/deadbeef', backupRoutes())
    expect(await screen.findByText(/There is no snapshot deadbeef/)).toBeTruthy()
  })
})

describe('file browser', () => {
  it('browses folders', async () => {
    const { location } = renderApp('/backup/incremental/3c9b8a7e/files', backupRoutes())
    expect(await screen.findByText('notes.txt')).toBeTruthy()
    expect(screen.getByText('Symlink')).toBeTruthy()
    expect(screen.getByText('2.0 KB')).toBeTruthy()
    expect(screen.queryByRole('link', { name: 'conf' })).toBeNull()
    fireEvent.click(screen.getByRole('link', { name: 'Documents' }))
    expect(await screen.findByText('invoice-2026-09.pdf')).toBeTruthy()
    expect(location()).toBe('/backup/incremental/3c9b8a7e/files')
    expect(screen.getByRole('button', { name: 'Up one level' })).toBeTruthy()
  })

  it('shows the server refusal for a folder it will not list', async () => {
    renderApp(
      '/backup/incremental/3c9b8a7e/files',
      backupRoutes(
        {},
        {
          'GET /backup/incremental/3c9b8a7e/files?path=%2Fhome%2Fadmin': () => ({
            status: 403,
            body: { error: { code: 'forbidden', message: 'Error: not allowed' } },
          }),
        },
      ),
    )
    expect(await screen.findByText('Error: not allowed')).toBeTruthy()
  })
})

describe('snapshot list selection', () => {
  it('selects a range with shift-click', async () => {
    const snap = (id: string, day: string) => ({
      id: id.repeat(8),
      short_id: id,
      time: `2026-10-${day}T03:00:05+02:00`,
      hostname: 'h',
      paths: ['/home/admin'],
    })
    renderApp(
      '/backup/incremental',
      backupRoutes(
        {},
        { 'GET /backup/incremental': () => ({ body: { snapshots: [snap('aaaa1111', '23'), snap('bbbb2222', '22'), snap('cccc3333', '21'), snap('dddd4444', '20')] } }) },
      ),
    )
    await screen.findByRole('link', { name: 'aaaa1111' })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select snapshot aaaa1111' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select snapshot cccc3333' }), { shiftKey: true })
    for (const [id, on] of [['aaaa1111', 'true'], ['bbbb2222', 'true'], ['cccc3333', 'true'], ['dddd4444', 'false']])
      expect(screen.getByRole('checkbox', { name: `Select snapshot ${id}` }).getAttribute('aria-checked')).toBe(on)
  })
})

describe('per-file restore', () => {
  const files = '/backup/incremental/3c9b8a7e/files'
  const routes = (overrides = {}, user = {}) => backupRoutes(user, { ...toolsRoutes(), 'GET /bootstrap': backupRoutes(user)['GET /bootstrap'], ...overrides })
  const restoreKey = 'POST /backup/incremental/3c9b8a7e/restore-files'

  it('offers Restore on files and folders, not on conf or the up row', async () => {
    renderApp(files, routes())
    await screen.findByText('notes.txt')
    for (const n of ['notes.txt', 'Documents', 'web', '.bash_history', 'current'])
      expect(screen.getByRole('button', { name: `Restore ${n}` })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Restore conf' })).toBeNull()
    expect(screen.getAllByRole('button', { name: /^Restore / })).toHaveLength(5)
  })

  it('has no restore controls while read-only', async () => {
    renderApp(files, routes({}, { readOnly: true }))
    await screen.findByText('notes.txt')
    expect(screen.queryByRole('button', { name: /^Restore/ })).toBeNull()
    expect(screen.queryAllByRole('checkbox')).toHaveLength(0)
  })

  it('confirms the overwrite, restores one file and shows the result', async () => {
    let polls = 0
    const { calls } = renderApp(
      files,
      routes({
        [`GET /backup/restore-jobs/${RESTORE_JOB_ID}`]: () => ({
          body:
            ++polls === 1
              ? { ...restoreJobDone([]), state: 'running', finished: '', step: '/home/admin/notes.txt' }
              : restoreJobDone(['/home/admin/notes.txt']),
        }),
      }),
    )
    await screen.findByText('notes.txt')
    fireEvent.click(screen.getByRole('button', { name: 'Restore notes.txt' }))
    const dialog = await screen.findByRole('dialog', { name: 'Restore files' })
    expect(within(dialog).getByText(/live file will be overwritten/)).toBeTruthy()
    expect(calls.some((c) => c.key === restoreKey)).toBe(false)
    fireEvent.click(within(dialog).getByRole('button', { name: 'Restore' }))
    await waitFor(() => expect(calls.find((c) => c.key === restoreKey)?.body).toEqual({ paths: ['/home/admin/notes.txt'] }))
    expect(await screen.findByText('Restoring files')).toBeTruthy()
    expect(await screen.findByText('Restored 1 file', {}, { timeout: 3000 })).toBeTruthy()
    expect(screen.getAllByText('/home/admin/notes.txt').length).toBeGreaterThan(0)
  })

  it('says per-file restore is turned off and stops offering it', async () => {
    const off = 'Restoring single files is turned off on this server.'
    renderApp(
      files,
      routes({ [restoreKey]: () => ({ status: 409, body: { error: { code: 'disabled', message: off } } }) }),
    )
    await screen.findByText('notes.txt')
    fireEvent.click(screen.getByRole('button', { name: 'Restore notes.txt' }))
    const dialog = await screen.findByRole('dialog', { name: 'Restore files' })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Restore' }))
    expect(await screen.findByText(off)).toBeTruthy()
    expect(screen.getByText('Whole items can still be restored from the snapshot page.')).toBeTruthy()
    await waitFor(() => expect(screen.getByRole('button', { name: 'Restore notes.txt' }).hasAttribute('disabled')).toBe(true))
  })

  it('restores every selected path in one call', async () => {
    const { calls } = renderApp(
      files,
      routes({ [`GET /backup/restore-jobs/${RESTORE_JOB_ID}`]: () => ({ body: restoreJobDone(['/home/admin/notes.txt', '/home/admin/web']) }) }),
    )
    await screen.findByText('notes.txt')
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select notes.txt' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select web' }))
    fireEvent.click(screen.getByRole('button', { name: 'Restore selected' }))
    const dialog = await screen.findByRole('dialog', { name: 'Restore files' })
    expect(within(dialog).getByText(/live files will be overwritten/)).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Restore' }))
    await waitFor(() => expect(calls.find((c) => c.key === restoreKey)?.body).toEqual({ paths: ['/home/admin/web', '/home/admin/notes.txt'] }))
    expect(await screen.findByText('Restored 2 files')).toBeTruthy()
  })

  it('selects a range with shift-click', async () => {
    renderApp(files, routes())
    await screen.findByText('notes.txt')
    const boxes = () => screen.getAllByRole('checkbox').filter((c) => /^Select (?!all)/.test(c.getAttribute('aria-label') ?? ''))
    fireEvent.click(boxes()[0])
    fireEvent.click(boxes()[3], { shiftKey: true })
    expect(boxes().filter((c) => c.getAttribute('aria-checked') === 'true')).toHaveLength(4)
  })

  it('disables Restore for names with spaces or wildcards', async () => {
    const node = (name: string) => ({ name, type: 'file', path: `/home/admin/${name}`, size: 1, ctime: '2026-10-18T09:12:44+02:00' })
    renderApp(
      files,
      routes({
        [`GET /backup/incremental/3c9b8a7e/files?path=${encodeURIComponent('/home/admin')}`]: () => ({
          body: { path: '/home/admin', parent: '', nodes: [node('my notes.txt'), node('a*b'), node('ok.txt')] },
        }),
      }),
    )
    await screen.findByText('ok.txt')
    for (const n of ['my notes.txt', 'a*b']) {
      expect(screen.getByRole('button', { name: `Restore ${n}` }).hasAttribute('disabled')).toBe(true)
      expect(screen.queryByRole('checkbox', { name: `Select ${n}` })).toBeNull()
    }
    expect(screen.getByRole('button', { name: 'Restore ok.txt' }).hasAttribute('disabled')).toBe(false)
    fireEvent.focus(screen.getByTestId('restore-hint-my notes.txt'))
    expect(await screen.findByText("Files with spaces or special characters in their names can't be restored from here.")).toBeTruthy()
  })

  it('names the failing path and the restored ones', async () => {
    renderApp(
      files,
      routes({
        [`GET /backup/restore-jobs/${RESTORE_JOB_ID}`]: () => ({
          body: restoreJobFailed('/home/admin/notes.txt', ['/home/admin/web']),
        }),
      }),
    )
    await screen.findByText('notes.txt')
    fireEvent.click(screen.getByRole('button', { name: 'Restore notes.txt' }))
    fireEvent.click(within(await screen.findByRole('dialog', { name: 'Restore files' })).getByRole('button', { name: 'Restore' }))
    expect(await screen.findByText('Error: Unable to download snapshot data')).toBeTruthy()
    expect(screen.getByText('Could not restore /home/admin/notes.txt')).toBeTruthy()
    expect(screen.getByText('/home/admin/web')).toBeTruthy()
    expect(document.body.textContent).not.toMatch(/Hestia/)
  })

  it.each([
    "%s goes through a link on the server and can't be restored.",
    "%s can't be checked on the server, so it can't be restored.",
  ])('fills the path into the job error %s', async (message) => {
    renderApp(
      files,
      routes({
        [`GET /backup/restore-jobs/${RESTORE_JOB_ID}`]: () => ({
          body: restoreJobFailed('/home/admin/notes.txt', [], message),
        }),
      }),
    )
    await screen.findByText('notes.txt')
    fireEvent.click(screen.getByRole('button', { name: 'Restore notes.txt' }))
    fireEvent.click(within(await screen.findByRole('dialog', { name: 'Restore files' })).getByRole('button', { name: 'Restore' }))
    expect(await screen.findByText(message.replace('%s', '/home/admin/notes.txt'))).toBeTruthy()
    expect(document.body.textContent).not.toContain('%s')
  })

  const homeRoute = (nodes: unknown[]) => ({
    [`GET /backup/incremental/3c9b8a7e/files?path=${encodeURIComponent('/home/admin')}`]: () => ({
      body: { path: '/home/admin', parent: '', nodes },
    }),
  })
  const node = (name: string, type = 'file', dir = '/home/admin') => ({
    name,
    type,
    path: `${dir}/${name}`,
    size: 1,
    ctime: '2026-10-18T09:12:44+02:00',
  })

  it('disables Restore when the whole path would be refused', async () => {
    renderApp(
      `${files}?path=${encodeURIComponent('/home/admin/we]ird')}`,
      routes({
        [`GET /backup/incremental/3c9b8a7e/files?path=${encodeURIComponent('/home/admin/we]ird')}`]: () => ({
          body: { path: '/home/admin/we]ird', parent: '/home/admin', nodes: [node('ok.txt', 'file', '/home/admin/we]ird')] },
        }),
        [`GET /backup/incremental/3c9b8a7e/files?path=${encodeURIComponent('/home/admin/-x')}`]: () => ({
          body: { path: '/home/admin/-x', parent: '/home/admin', nodes: [node('ok.txt', 'file', '/home/admin/-x')] },
        }),
      }),
    )
    await screen.findByText('ok.txt')
    expect(screen.getByRole('button', { name: 'Restore ok.txt' }).hasAttribute('disabled')).toBe(true)
    cleanup()
    renderApp(`${files}?path=${encodeURIComponent('/home/admin/-x')}`, routes({
      [`GET /backup/incremental/3c9b8a7e/files?path=${encodeURIComponent('/home/admin/-x')}`]: () => ({
        body: { path: '/home/admin/-x', parent: '/home/admin', nodes: [node('ok.txt', 'file', '/home/admin/-x')] },
      }),
    }))
    await screen.findByText('ok.txt')
    expect(screen.getByRole('button', { name: 'Restore ok.txt' }).hasAttribute('disabled')).toBe(true)
    expect(screen.queryByRole('checkbox', { name: 'Select ok.txt' })).toBeNull()
  })

  it('never selects conf or unsafe rows, and select-all counts only the rest', async () => {
    const { calls } = renderApp(
      files,
      routes(homeRoute([node('conf', 'dir'), node('a b.txt'), node('x*y'), node('one.txt'), node('two.txt')])),
    )
    await screen.findByText('one.txt')
    expect(screen.queryByRole('checkbox', { name: 'Select conf' })).toBeNull()
    expect(screen.queryByRole('checkbox', { name: 'Select a b.txt' })).toBeNull()
    expect(screen.queryByRole('checkbox', { name: 'Select x*y' })).toBeNull()
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select all items' }))
    expect(screen.getByText('2 selected')).toBeTruthy()
    expect(screen.getByRole('checkbox', { name: 'Select all items' }).getAttribute('aria-checked')).toBe('true')
    fireEvent.click(screen.getByRole('button', { name: 'Restore selected' }))
    fireEvent.click(within(await screen.findByRole('dialog', { name: 'Restore files' })).getByRole('button', { name: 'Restore' }))
    await waitFor(() =>
      expect(calls.find((c) => c.key === restoreKey)?.body).toEqual({
        paths: ['/home/admin/one.txt', '/home/admin/two.txt'],
      }),
    )
  })

  it('restores a shift-click range without conf or unsafe rows', async () => {
    const { calls } = renderApp(
      files,
      routes(homeRoute([node('conf', 'dir'), node('a b.txt'), node('one.txt'), node('two.txt')])),
    )
    await screen.findByText('one.txt')
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select one.txt' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select two.txt' }), { shiftKey: true })
    expect(screen.getByText('2 selected')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Restore selected' }))
    fireEvent.click(within(await screen.findByRole('dialog', { name: 'Restore files' })).getByRole('button', { name: 'Restore' }))
    await waitFor(() =>
      expect(calls.find((c) => c.key === restoreKey)?.body).toEqual({ paths: ['/home/admin/one.txt', '/home/admin/two.txt'] }),
    )
  })

  it('caps one restore at 50 files', async () => {
    const many = Array.from({ length: 60 }, (_, i) => node(`f${String(i).padStart(2, '0')}.txt`))
    renderApp(files, routes(homeRoute(many)))
    await screen.findByText('f00.txt')
    for (let page = 0; page < 3; page++) {
      fireEvent.click(screen.getByRole('checkbox', { name: 'Select all items' }))
      if (page < 2) fireEvent.click(screen.getByRole('button', { name: /next/i }))
    }
    const bulk = screen.getAllByRole('button', { name: 'Restore selected' }).at(-1)!
    expect(bulk.hasAttribute('disabled')).toBe(true)
    fireEvent.focus(screen.getByTestId('restore-cap'))
    expect(await screen.findByText('You can restore up to 50 files at once.')).toBeTruthy()
  })

  it('clears the selection once the job starts', async () => {
    renderApp(files, routes())
    await screen.findByText('notes.txt')
    fireEvent.click(screen.getByRole('checkbox', { name: 'Select notes.txt' }))
    fireEvent.click(screen.getByRole('button', { name: 'Restore selected' }))
    fireEvent.click(within(await screen.findByRole('dialog', { name: 'Restore files' })).getByRole('button', { name: 'Restore' }))
    await waitFor(() => expect(screen.queryByText('1 selected')).toBeNull())
  })
})

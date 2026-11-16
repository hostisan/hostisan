import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { keysRoutes, notification } from '../../test/accountFixtures'
import { renderApp } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const ok = () => ({ body: { status: 'ok' } })

describe('notifications page', () => {
  it('lists the notifications from the API with safe markup', async () => {
    renderApp(
      '/notifications',
      keysRoutes(
        {},
        {
          'GET /notifications': () => ({
            body: {
              items: [
                notification('2', { NOTICE: '<p>Bad <b>news</b><script>window.pwned=1</script></p>' }),
                notification('1', { TOPIC: 'Welcome to Hostisan', ACK: 'yes', NOTICE: '<p>Hi <a href="javascript:alert(1)">there</a></p>' }),
              ],
            },
          }),
        },
      ),
    )
    expect(await screen.findByText('Backup failed')).toBeTruthy()
    expect(screen.getByText('Welcome to Hostisan')).toBeTruthy()
    expect(screen.getByText('news').tagName).toBe('B')
    expect(document.querySelector('script')).toBeNull()
    expect(document.querySelector('a[href^="javascript"]')).toBeNull()
    expect(screen.getAllByText('New')).toHaveLength(1)
  })

  it('shows the error from the API', async () => {
    renderApp(
      '/notifications',
      keysRoutes(
        {},
        { 'GET /notifications': () => ({ status: 500, body: { error: { code: 'server_error', message: 'Error: boom' } } }) },
      ),
    )
    expect(await screen.findByText('Error: boom')).toBeTruthy()
  })

  it('marks one as read', async () => {
    const app = renderApp('/notifications', keysRoutes({}, { 'POST /notifications/2/ack': ok }))
    await screen.findByText('Backup failed')
    fireEvent.click(screen.getByRole('button', { name: 'Actions for Backup failed' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Mark as read' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /notifications/2/ack')).toBe(true))
  })

  it('marks all as read', async () => {
    const app = renderApp('/notifications', keysRoutes({}, { 'POST /notifications/ack': ok }))
    await screen.findByText('Backup failed')
    fireEvent.click(screen.getByRole('button', { name: 'Mark all as read' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'POST /notifications/ack')).toBe(true))
  })

  it('deletes one, and all after confirming', async () => {
    const app = renderApp(
      '/notifications',
      keysRoutes({}, { 'DELETE /notifications/2': ok, 'DELETE /notifications': ok }),
    )
    await screen.findByText('Backup failed')
    fireEvent.click(screen.getByRole('button', { name: 'Actions for Backup failed' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'DELETE /notifications/2')).toBe(true))

    fireEvent.click(screen.getByRole('button', { name: 'Delete all' }))
    const dialog = await screen.findByRole('dialog', { name: 'Delete all notifications?' })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete all' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'DELETE /notifications')).toBe(true))
  })

  it('has no write controls while read-only', async () => {
    renderApp('/notifications', keysRoutes({ readOnly: true, role: 'user', landing: '/web' }))
    await screen.findByText('Backup failed')
    expect(screen.queryByRole('button', { name: 'Mark all as read' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Delete all' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Actions for Backup failed' })).toBeNull()
  })

  it('runs bulk mark-as-read and delete one call at a time', async () => {
    const items = [notification('3'), notification('2'), notification('1', { ACK: 'no' })]
    const app = renderApp(
      '/notifications',
      keysRoutes(
        {},
        {
          'GET /notifications': () => ({ body: { items } }),
          'POST /notifications/3/ack': ok,
          'POST /notifications/2/ack': ok,
          'POST /notifications/1/ack': ok,
          'DELETE /notifications/3': ok,
          'DELETE /notifications/2': ok,
          'DELETE /notifications/1': ok,
        },
      ),
    )
    await screen.findAllByText('Backup failed')
    // the server edits one config file per call: never two writes at once
    const inner = globalThis.fetch
    let inflight = 0
    let max = 0
    vi.stubGlobal('fetch', async (url: string, init?: RequestInit) => {
      const write = (init?.method ?? 'GET') !== 'GET'
      if (write) max = Math.max(max, ++inflight)
      await new Promise((r) => setTimeout(r, 15))
      try {
        return await inner(url, init)
      } finally {
        if (write) inflight--
      }
    })
    fireEvent.click(screen.getAllByRole('checkbox')[0])
    fireEvent.click(screen.getByRole('button', { name: 'Mark as read' }))
    await waitFor(() => expect(app.calls.filter((c) => c.key.endsWith('/ack'))).toHaveLength(3))
    expect(max).toBe(1)
  })
})

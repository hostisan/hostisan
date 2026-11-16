import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { keysRoutes } from '../test/accountFixtures'
import { notification } from '../test/accountFixtures'
import { renderApp } from '../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const bell = (name: RegExp | string = /^Notifications/) => screen.findByRole('button', { name })

describe('notifications tray', () => {
  it('shows the badge from the session flag before anything is loaded, and loads on first click only', async () => {
    const app = renderApp('/error', keysRoutes({ notifications: true }))
    const b = await bell('Notifications, unread')
    expect(app.calls.some((c) => c.key === 'GET /notifications')).toBe(false)
    fireEvent.click(b)
    expect(await screen.findByText('Backup failed')).toBeTruthy()
    expect(app.calls.filter((c) => c.key === 'GET /notifications')).toHaveLength(1)
  })

  it('has no badge without the flag, and none once everything loaded is read', async () => {
    renderApp(
      '/error',
      keysRoutes({}, { 'GET /notifications': () => ({ body: { items: [notification('1', { ACK: 'yes' })] } }) }),
    )
    const b = await bell('Notifications')
    fireEvent.click(b)
    await screen.findByText('Backup failed')
    expect(screen.queryByRole('button', { name: 'Notifications, unread' })).toBeNull()
  })

  it('renders notice markup safely', async () => {
    renderApp(
      '/error',
      keysRoutes(
        {},
        {
          'GET /notifications': () => ({
            body: {
              items: [
                notification('1', {
                  NOTICE:
                    '<p>Read the <a href="https://docs.example.org/">docs</a><script>window.pwned=1</script><img src=x onerror="window.pwned=1"></p>',
                }),
              ],
            },
          }),
        },
      ),
    )
    fireEvent.click(await bell())
    const link = await screen.findByRole('link', { name: 'docs' })
    expect(link.getAttribute('href')).toBe('https://docs.example.org/')
    expect(document.querySelector('script, img[onerror]')).toBeNull()
  })

  it('deletes one notification and refetches', async () => {
    let items = [notification('2'), notification('1', { TOPIC: 'Welcome to Hostisan', ACK: 'yes' })]
    const app = renderApp(
      '/error',
      keysRoutes(
        {},
        {
          'GET /notifications': () => ({ body: { items } }),
          'DELETE /notifications/2': () => {
            items = items.filter((n) => n.ID !== '2')
            return { body: { status: 'ok' } }
          },
        },
      ),
    )
    fireEvent.click(await bell())
    await screen.findByText('Backup failed')
    // two items: no "delete all"
    expect(screen.queryByRole('button', { name: 'Delete all notifications' })).toBeNull()
    fireEvent.click(screen.getAllByRole('button', { name: 'Delete notification' })[0])
    await waitFor(() => expect(app.calls.some((c) => c.key === 'DELETE /notifications/2')).toBe(true))
    await waitFor(() => expect(screen.queryByText('Backup failed')).toBeNull())
    expect(screen.getByText('Welcome to Hostisan')).toBeTruthy()
  })

  it('offers delete all above two items', async () => {
    let items = [notification('3'), notification('2'), notification('1')]
    const app = renderApp(
      '/error',
      keysRoutes(
        {},
        {
          'GET /notifications': () => ({ body: { items } }),
          'DELETE /notifications': () => {
            items = []
            return { body: { status: 'ok' } }
          },
        },
      ),
    )
    fireEvent.click(await bell())
    fireEvent.click(await screen.findByRole('button', { name: 'Delete all notifications' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'DELETE /notifications')).toBe(true))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  it('is hidden for the protected admin (read-only admin)', async () => {
    renderApp('/error', keysRoutes({ readOnly: true, look: 'admin' }))
    await screen.findByText(/You are viewing the panel as admin/)
    expect(screen.queryByRole('button', { name: /^Notifications/ })).toBeNull()
  })

  it('names the empty state', async () => {
    renderApp('/error', keysRoutes({}, { 'GET /notifications': () => ({ body: { items: [] } }) }))
    fireEvent.click(await bell())
    const pop = await screen.findByText('No notifications')
    expect(within(pop.parentElement as HTMLElement)).toBeTruthy()
  })

  it('refetches when reopened and shows the dot for new unread items', async () => {
    let items = [notification('1', { ACK: 'yes' })]
    const app = renderApp('/error', keysRoutes({}, { 'GET /notifications': () => ({ body: { items } }) }))
    fireEvent.click(await bell('Notifications'))
    await screen.findByText('Backup failed')
    fireEvent.click(screen.getByRole('button', { name: 'Notifications' }))
    await waitFor(() => expect(screen.queryByText('Backup failed')).toBeNull())
    items = [notification('2', { TOPIC: 'Fresh one' }), ...items]
    fireEvent.click(screen.getByRole('button', { name: 'Notifications' }))
    expect(await screen.findByText('Fresh one')).toBeTruthy()
    expect(app.calls.filter((c) => c.key === 'GET /notifications')).toHaveLength(2)
    expect(screen.getByRole('button', { name: 'Notifications, unread' })).toBeTruthy()
  })

  it('closes the panel after delete all', async () => {
    let items = [notification('3'), notification('2'), notification('1')]
    const app = renderApp(
      '/error',
      keysRoutes(
        {},
        {
          'GET /notifications': () => ({ body: { items } }),
          'DELETE /notifications': () => {
            items = []
            return { body: { status: 'ok' } }
          },
        },
      ),
    )
    fireEvent.click(await bell())
    expect(await screen.findByRole('dialog')).toBeTruthy()
    fireEvent.click(await screen.findByRole('button', { name: 'Delete all notifications' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'DELETE /notifications')).toBe(true))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })

  it('closes the panel when the last item is removed', async () => {
    let items = [notification('1')]
    const app = renderApp(
      '/error',
      keysRoutes(
        {},
        {
          'GET /notifications': () => ({ body: { items } }),
          'DELETE /notifications/1': () => {
            items = []
            return { body: { status: 'ok' } }
          },
        },
      ),
    )
    fireEvent.click(await bell())
    expect(await screen.findByRole('dialog')).toBeTruthy()
    fireEvent.click(await screen.findByRole('button', { name: 'Delete notification' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'DELETE /notifications/1')).toBe(true))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
  })
})

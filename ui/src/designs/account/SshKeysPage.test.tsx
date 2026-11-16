import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { keysRoutes } from '../../test/accountFixtures'
import { renderApp } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('SSH keys list', () => {
  it('lists the keys from the API', async () => {
    renderApp('/account/ssh-keys', keysRoutes())
    expect(await screen.findByText('admin@workstation')).toBeTruthy()
    expect(screen.getByText('deploy key 2024')).toBeTruthy()
    expect(screen.getByText('SHA256:eU2u7tqL0ZkQ4yW3d8p0n5bXr1sHcVjMf6aTgOiBzKo')).toBeTruthy()
    expect(screen.getByText(/2 SSH keys/)).toBeTruthy()
  })

  it('shows the empty state', async () => {
    renderApp('/account/ssh-keys', keysRoutes({}, { 'GET /account/ssh-keys': () => ({ body: { keys: [] } }) }))
    expect(await screen.findByText('No SSH keys yet')).toBeTruthy()
  })

  it('shows the error from the API', async () => {
    renderApp(
      '/account/ssh-keys',
      keysRoutes({}, { 'GET /account/ssh-keys': () => ({ status: 500, body: { error: { code: 'x', message: 'Error: boom' } } }) }),
    )
    expect(await screen.findByText('Error: boom')).toBeTruthy()
  })

  it('deletes a key by its ID after confirming', async () => {
    const app = renderApp(
      '/account/ssh-keys',
      keysRoutes({}, { 'DELETE /account/ssh-keys/deploy%20key%202024': () => ({ body: { status: 'ok' } }) }),
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Delete deploy key 2024' }))
    const dialog = await screen.findByRole('dialog', { name: 'Delete SSH key' })
    expect(within(dialog).getByText(/delete SSH key deploy key 2024/)).toBeTruthy()
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'DELETE /account/ssh-keys/deploy%20key%202024')).toBe(true))
  })

  it('hides add and delete while read-only', async () => {
    renderApp('/account/ssh-keys', keysRoutes({ readOnly: true, role: 'user', landing: '/web' }))
    await screen.findByText('admin@workstation')
    expect(screen.queryByRole('link', { name: 'Add SSH key' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Delete admin@workstation' })).toBeNull()
  })
})

describe('add SSH key', () => {
  const type = (v: string) => fireEvent.change(screen.getByRole('textbox'), { target: { value: v } })

  it('posts the key and leaves the form empty', async () => {
    const app = renderApp(
      '/account/ssh-keys/new',
      keysRoutes({}, { 'POST /account/ssh-keys': () => ({ body: { status: 'ok' } }) }),
    )
    await screen.findByText('Public key')
    type('ssh-ed25519 AAAAC3 me@laptop')
    fireEvent.click(screen.getByRole('button', { name: 'Add SSH key' }))
    await waitFor(() =>
      expect(app.calls.find((c) => c.key === 'POST /account/ssh-keys')?.body).toEqual({ key: 'ssh-ed25519 AAAAC3 me@laptop' }),
    )
    await waitFor(() => expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe(''))
    expect(app.location()).toBe('/account/ssh-keys/new')
    expect(await screen.findByText('SSH Key has been created successfully.')).toBeTruthy() // Hestia's msgid (ruling 19)
  })

  it('shows the duplicate error and keeps the text', async () => {
    renderApp(
      '/account/ssh-keys/new',
      keysRoutes(
        {},
        {
          'POST /account/ssh-keys': () => ({
            status: 409,
            body: { error: { code: 'exists', message: 'SSH Key already exists.' } },
          }),
        },
      ),
    )
    await screen.findByText('Public key')
    type('ssh-ed25519 AAAAC3 me@laptop')
    fireEvent.click(screen.getByRole('button', { name: 'Add SSH key' }))
    expect(await screen.findByText('SSH Key already exists.')).toBeTruthy()
    expect((screen.getByRole('textbox') as HTMLTextAreaElement).value).toBe('ssh-ed25519 AAAAC3 me@laptop')
  })

  it('does not submit a blank key', async () => {
    const app = renderApp('/account/ssh-keys/new', keysRoutes())
    await screen.findByText('Public key')
    expect((screen.getByRole('button', { name: 'Add SSH key' }) as HTMLButtonElement).disabled).toBe(true)
    expect(app.calls.some((c) => c.key.startsWith('POST'))).toBe(false)
  })
})

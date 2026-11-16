import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { anonymous, renderApp, signedIn } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

async function typeAndSubmit(label: string, value: string) {
  const input = await screen.findByLabelText(label)
  fireEvent.change(input, { target: { value } })
  fireEvent.submit(input.closest('form')!)
}

describe('LoginPage', () => {
  it('signs in through the password and 2FA steps', async () => {
    let authed = false
    const app = renderApp('/login', {
      'GET /bootstrap': () => ({ body: authed ? signedIn({ name: 'bob', role: 'user', landing: '/dns' }) : anonymous }),
      'POST /auth/login': (body) => {
        const b = body as { username?: string; twofa?: string }
        if (b.username === 'bob') return { body: { status: 'twofa_required' } }
        authed = true
        return { body: { status: 'ok', landing: '/dns' } }
      },
    })
    await typeAndSubmit('Username', 'bob')
    const password = await screen.findByLabelText('Password')
    fireEvent.change(password, { target: { value: 's3cret' } })
    fireEvent.submit(password.closest('form')!)
    await typeAndSubmit('2FA token', '123456')
    await waitFor(() => expect(app.location()).toBe('/dns'))
    expect(app.calls.filter((c) => c.key === 'POST /auth/login').map((c) => c.body)).toEqual([
      { username: 'bob', password: 's3cret' },
      { twofa: '123456' },
    ])
  })

  it('shows the error from the server', async () => {
    renderApp('/login', {
      'GET /bootstrap': () => ({ body: anonymous }),
      'POST /auth/login': () => ({ status: 401, body: { error: { code: 'login_failed', message: 'Invalid username or password' } } }),
    })
    await typeAndSubmit('Username', 'ghost')
    const password = await screen.findByLabelText('Password')
    fireEvent.change(password, { target: { value: 'x' } })
    fireEvent.submit(password.closest('form')!)
    expect(await screen.findByText('Invalid username or password')).toBeTruthy()
  })

  it('shows no demo credentials', async () => {
    renderApp('/login', { 'GET /bootstrap': () => ({ body: anonymous }) })
    await screen.findByLabelText('Username')
    expect(screen.queryByText(/Design preview/)).toBeNull()
  })

  it('links to the password reset only when the policy allows it', async () => {
    renderApp('/login', { 'GET /bootstrap': () => ({ body: anonymous }) })
    await typeAndSubmit('Username', 'bob')
    expect(await screen.findAllByText('Forgot your password?')).not.toHaveLength(0)
    cleanup()
    renderApp('/login', { 'GET /bootstrap': () => ({ body: { ...anonymous, passwordReset: false } }) })
    await screen.findByLabelText('Username')
    expect(screen.queryByText('Forgot your password?')).toBeNull()
    await typeAndSubmit('Username', 'bob')
    await screen.findByLabelText('Password')
    expect(screen.queryByText('Forgot your password?')).toBeNull()
  })

  it('links the 2FA step to the unlock page', async () => {
    renderApp('/login', {
      'GET /bootstrap': () => ({ body: anonymous }),
      'POST /auth/login': () => ({ body: { status: 'twofa_required' } }),
    })
    await typeAndSubmit('Username', 'bob')
    const password = await screen.findByLabelText('Password')
    fireEvent.change(password, { target: { value: 'x' } })
    fireEvent.submit(password.closest('form')!)
    const link = await screen.findByText('Forgot token?')
    expect(link.closest('a')?.getAttribute('href')).toBe('/reset-2fa')
  })
})

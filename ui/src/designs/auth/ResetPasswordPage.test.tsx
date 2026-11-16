import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { recoveryRoutes } from '../../test/accountFixtures'
import { renderApp } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

async function fill(label: string, value: string) {
  const input = await screen.findByLabelText(label)
  fireEvent.change(input, { target: { value } })
  return input
}

async function submit(label: string, value?: string) {
  const input = value === undefined ? await screen.findByLabelText(label) : await fill(label, value)
  fireEvent.submit(input.closest('form')!)
}

const apiError = (message: string) => ({ status: 400, body: { error: { code: 'invalid', message } } })

describe('ResetPasswordPage', () => {
  it('walks the request, code, password and done steps', async () => {
    const app = renderApp('/reset', recoveryRoutes())
    await fill('Username', 'bob')
    await submit('Email', 'bob@acme-bakery.com')
    expect(await screen.findByText(/Password reset instructions have been sent/)).toBeTruthy()
    await submit('Reset code', 'K7Q2M9XD')
    await fill('New password', 'Secr3tPass')
    await submit('Confirm password', 'Secr3tPass')
    expect(await screen.findByText('Password changed')).toBeTruthy()
    expect(app.calls.filter((c) => c.key.startsWith('POST /auth/reset')).map((c) => [c.key, c.body])).toEqual([
      ['POST /auth/reset/request', { user: 'bob', email: 'bob@acme-bakery.com' }],
      [
        'POST /auth/reset/confirm',
        { user: 'bob', code: 'K7Q2M9XD', password: 'Secr3tPass', passwordConfirm: 'Secr3tPass' },
      ],
    ])
    fireEvent.click(screen.getByRole('button', { name: 'Log in' }))
    await waitFor(() => expect(app.location()).toBe('/login'))
  })

  it('opens the password step from a confirm link', async () => {
    const app = renderApp('/reset/?action=confirm&user=bob&code=ABC123', recoveryRoutes())
    await fill('New password', 'Secr3tPass')
    await submit('Confirm password', 'Secr3tPass')
    expect(await screen.findByText('Password changed')).toBeTruthy()
    expect(app.calls.find((c) => c.key === 'POST /auth/reset/confirm')?.body).toEqual({
      user: 'bob',
      code: 'ABC123',
      password: 'Secr3tPass',
      passwordConfirm: 'Secr3tPass',
    })
  })

  it('opens the code step from a code link with the user filled in', async () => {
    renderApp('/reset?action=code&user=bob', recoveryRoutes())
    expect(await screen.findByLabelText('Reset code')).toBeTruthy()
    expect(screen.getByText('bob')).toBeTruthy()
  })

  it('shows the API message and returns to the code step when the code is refused', async () => {
    renderApp(
      '/reset?action=confirm&user=bob&code=BAD',
      recoveryRoutes({}, { 'POST /auth/reset/confirm': () => apiError('Invalid username or code') }),
    )
    await fill('New password', 'Secr3tPass')
    await submit('Confirm password', 'Secr3tPass')
    expect(await screen.findByText('Invalid username or code')).toBeTruthy()
    expect(await screen.findByLabelText('Reset code')).toBeTruthy()
  })

  it('shows a password error and stays on the password step', async () => {
    renderApp(
      '/reset?action=confirm&user=bob&code=OK',
      recoveryRoutes({}, { 'POST /auth/reset/confirm': () => apiError('Password does not match the minimum requirements.') }),
    )
    await fill('New password', 'weak')
    await submit('Confirm password', 'weak')
    expect(await screen.findByText('Password does not match the minimum requirements.')).toBeTruthy()
    expect(screen.getByLabelText('New password')).toBeTruthy()
  })
})

describe('ResetPasswordPage when password reset is disabled', () => {
  it('redirects to the login page, as Hestia does', async () => {
    const app = renderApp('/reset', recoveryRoutes({ passwordReset: false }))
    await waitFor(() => expect(app.location()).toBe('/login'))
  })
})

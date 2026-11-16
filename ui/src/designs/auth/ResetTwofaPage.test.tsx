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

describe('ResetTwofaPage', () => {
  it('asks for the password, then the emailed code', async () => {
    const app = renderApp('/reset-2fa', recoveryRoutes())
    await fill('Username', 'bob')
    const password = await fill('Password', 'Secr3tPass')
    fireEvent.submit(password.closest('form')!)
    expect(await screen.findByText(/an unlock code has been sent/)).toBeTruthy()
    const code = await fill('Unlock code', 'ABCDEF')
    fireEvent.submit(code.closest('form')!)
    expect(await screen.findByText('Account unlocked')).toBeTruthy()
    expect(app.calls.filter((c) => c.key.startsWith('POST /auth/reset-2fa')).map((c) => [c.key, c.body])).toEqual([
      ['POST /auth/reset-2fa/request', { user: 'bob', password: 'Secr3tPass' }],
      ['POST /auth/reset-2fa/confirm', { user: 'bob', code: 'ABCDEF' }],
    ])
    fireEvent.click(screen.getByRole('button', { name: 'Log in' }))
    await waitFor(() => expect(app.location()).toBe('/login'))
  })

  it('opens the code step from an email link', async () => {
    renderApp('/reset-2fa/?action=confirm&user=bob&code=XYZ', recoveryRoutes())
    const code = (await screen.findByLabelText('Unlock code')) as HTMLInputElement
    expect(code.value).toBe('XYZ')
  })

  it('shows the API message for a refused code', async () => {
    renderApp(
      '/reset-2fa?action=code&user=bob',
      recoveryRoutes(
        {},
        { 'POST /auth/reset-2fa/confirm': () => ({ status: 400, body: { error: { code: 'invalid', message: 'Invalid username or code' } } }) },
      ),
    )
    const code = await fill('Unlock code', 'NOPE')
    fireEvent.submit(code.closest('form')!)
    expect(await screen.findByText('Invalid username or code')).toBeTruthy()
  })
})

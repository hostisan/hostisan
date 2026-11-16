import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, ApiError, setCsrfToken, setSessionLostHandler } from './client'

function mockFetch(status: number, body: unknown) {
  const fn = vi.fn(async () => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }))
  vi.stubGlobal('fetch', fn)
  return fn
}

afterEach(() => {
  vi.unstubAllGlobals()
  setCsrfToken('')
  setSessionLostHandler(null)
})

describe('api client', () => {
  it('sends the CSRF token and JSON body on POST', async () => {
    const fetch = mockFetch(200, { status: 'ok' })
    setCsrfToken('tok123')
    await api.post('/auth/login', { username: 'admin' })
    const [url, init] = fetch.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('/api/v1/auth/login')
    expect(new Headers(init.headers).get('X-CSRF-Token')).toBe('tok123')
    expect(init.body).toBe(JSON.stringify({ username: 'admin' }))
    expect(init.credentials).toBe('same-origin')
  })

  it('sends a JSON body on DELETE when given one', async () => {
    const fetch = mockFetch(200, { status: 'ok' })
    await api.del('/account/2fa', { password: 'pw' })
    const [, init] = fetch.mock.calls[0] as unknown as [string, RequestInit]
    expect(init.method).toBe('DELETE')
    expect(init.body).toBe(JSON.stringify({ password: 'pw' }))
  })

  it('returns parsed JSON on success', async () => {
    mockFetch(200, { authenticated: false })
    await expect(api.get('/bootstrap')).resolves.toEqual({ authenticated: false })
  })

  it('throws ApiError with the code and message', async () => {
    mockFetch(401, { error: { code: 'login_failed', message: 'Invalid username or password' } })
    const err = await api.post('/auth/login', {}).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 401, code: 'login_failed', message: 'Invalid username or password' })
  })

  it('reports a lost session', async () => {
    mockFetch(401, { error: { code: 'logged_out', message: 'You are logged out, please log in again.' } })
    const lost = vi.fn()
    setSessionLostHandler(lost)
    await api.get('/anything').catch(() => {})
    expect(lost).toHaveBeenCalledWith('You are logged out, please log in again.')
  })

  it('does not report a failed login as a lost session', async () => {
    mockFetch(401, { error: { code: 'login_failed', message: 'Invalid username or password' } })
    const lost = vi.fn()
    setSessionLostHandler(lost)
    await api.post('/auth/login', {}).catch(() => {})
    expect(lost).not.toHaveBeenCalled()
  })
})

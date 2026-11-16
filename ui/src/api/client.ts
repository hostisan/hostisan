// Thin fetch wrapper for Hostisan's API: same-origin cookies, the CSRF header on
// writes, and typed errors.

const BASE = '/api/v1'

export class ApiError extends Error {
  status: number
  code: string
  /** English msgids that fill the %s in message; empty for Hestia's own output. */
  args: string[]
  constructor(status: number, code: string, message: string, args: string[] = []) {
    super(message)
    this.args = args
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

let csrfToken = ''
let sessionLost: ((message: string) => void) | null = null

/** Set from each bootstrap response; sent as X-CSRF-Token on every request. */
export function setCsrfToken(token: string) {
  csrfToken = token
}

/** Called when the server ends the session (timeout, IP change, suspension…). */
export function setSessionLostHandler(handler: ((message: string) => void) | null) {
  sessionLost = handler
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (csrfToken) headers['X-CSRF-Token'] = csrfToken
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const res = await fetch(BASE + path, {
    method,
    headers,
    credentials: 'same-origin',
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data = await res.json().catch(() => null)
  if (!res.ok) {
    const code: string = data?.error?.code ?? 'server_error'
    const message: string = data?.error?.message ?? ''
    if (res.status === 401 && (code === 'session_expired' || code === 'logged_out')) sessionLost?.(message)
    const args: string[] = Array.isArray(data?.error?.args) ? data.error.args : []
    throw new ApiError(res.status, code, message, args)
  }
  return data as T
}

export const api = {
  get: <T>(path: string) => request<T>('GET', path),
  post: <T>(path: string, body?: unknown) => request<T>('POST', path, body),
  put: <T>(path: string, body?: unknown) => request<T>('PUT', path, body),
  del: <T>(path: string, body?: unknown) => request<T>('DELETE', path, body),
}

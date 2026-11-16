import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { accessKey, keysRoutes } from '../../test/accountFixtures'
import source from './AccessKeysPage.tsx?raw'
import { renderApp } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const ok = () => ({ body: { status: 'ok' } })
const NEW_ID = 'Nw1Ky2Ab3Cd4Ef5Gh6Ij'
const SECRET = 'DevNewSecretNotReal0123456789abcdefABCD'
const slow = { timeout: 10000 }

describe('access key list', () => {
  it('lists the keys newest first, with comment and permissions', async () => {
    renderApp('/account/access-keys', keysRoutes())
    await screen.findByRole('link', { name: 'aB3dEfGh1JkLmN0pQrSt' })
    const ids = screen.getAllByRole('link').map((a) => a.textContent).filter((t) => /^[A-Za-z0-9]{20}$/.test(t ?? ''))
    expect(ids).toEqual(['Kp4Lm7Nq2Rs5Tv8Wx1Yz', 'Zx9yWv8uTs7rQp6oNm5l', 'aB3dEfGh1JkLmN0pQrSt'])
    expect(screen.getByText('Billing sync')).toBeTruthy()
    expect(screen.getByText('purge-nginx-cache')).toBeTruthy()
  })

  it('shows the 403 from the API gate', async () => {
    renderApp(
      '/account/access-keys',
      keysRoutes({}, { 'GET /account/access-keys': () => ({ status: 403, body: { error: { code: 'forbidden', message: '' } } }) }),
    )
    expect(await screen.findByText(/You don't have permission to view this page/)).toBeTruthy()
  })

  it('deletes one key from the row menu', async () => {
    const app = renderApp(
      '/account/access-keys',
      keysRoutes({}, { 'DELETE /account/access-keys/aB3dEfGh1JkLmN0pQrSt': ok }),
    )
    await screen.findByRole('link', { name: 'aB3dEfGh1JkLmN0pQrSt' })
    fireEvent.click(screen.getByRole('button', { name: 'Actions for aB3dEfGh1JkLmN0pQrSt' }))
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog', { name: 'Delete access key' })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(app.calls.some((c) => c.key === 'DELETE /account/access-keys/aB3dEfGh1JkLmN0pQrSt')).toBe(true))
  })

  it('deletes the selected keys in one bulk call and names failures', async () => {
    const app = renderApp(
      '/account/access-keys',
      keysRoutes(
        {},
        {
          'POST /account/access-keys/bulk': () => ({
            body: { status: 'ok', failed: [{ name: 'Zx9yWv8uTs7rQp6oNm5l', message: 'Error: nope' }] },
          }),
        },
      ),
    )
    await screen.findByRole('link', { name: 'aB3dEfGh1JkLmN0pQrSt' })
    // [select all, Kp4..., Zx9..., aB3...]: newest first
    const boxes = screen.getAllByRole('checkbox')
    fireEvent.click(boxes[2])
    fireEvent.click(boxes[3])
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    const dialog = await screen.findByRole('dialog', { name: 'Delete access key' })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Delete' }))
    await waitFor(() =>
      expect(app.calls.find((c) => c.key === 'POST /account/access-keys/bulk')?.body).toEqual({
        action: 'delete',
        names: ['Zx9yWv8uTs7rQp6oNm5l', 'aB3dEfGh1JkLmN0pQrSt'],
      }),
    )
    expect(await screen.findByText('Error: nope')).toBeTruthy()
  })
})

describe('access key view', () => {
  it('shows the permissions and never a secret', async () => {
    renderApp('/account/access-keys/aB3dEfGh1JkLmN0pQrSt', keysRoutes())
    expect((await screen.findAllByText('Billing sync')).length).toBeGreaterThan(0)
    expect(screen.getAllByText('billing').length).toBeGreaterThan(0)
    expect(screen.queryByText(/Last chance to save secret key/)).toBeNull()
  })

  it('says when the key does not exist', async () => {
    renderApp('/account/access-keys/missing', keysRoutes())
    expect(await screen.findByText('Access key not found')).toBeTruthy()
  })
})

describe('add access key', () => {
  const routes = () =>
    keysRoutes(
      {},
      {
        'POST /account/access-keys': () => ({
          body: { key: accessKey(NEW_ID, { COMMENT: 'New key' }), secret: SECRET },
        }),
        [`GET /account/access-keys/${NEW_ID}`]: () => ({ body: accessKey(NEW_ID, { COMMENT: 'New key' }) }),
      },
    )

  it('offers the APIs the server listed and needs one', async () => {
    renderApp('/account/access-keys/new', routes())
    await screen.findByRole('checkbox', { name: /mail-accounts/ })
    expect(screen.getByRole('checkbox', { name: /billing/ })).toBeTruthy()
    expect((screen.getByRole('button', { name: 'Add access key' }) as HTMLButtonElement).disabled).toBe(true)
  })

  // Four waits in a row across a POST and a route change: on a loaded machine (load
  // 15–18) one of them runs past the 3 s default, so each gets the time it needs.
  it('creates the key and shows the secret once', async () => {
    const app = renderApp('/account/access-keys/new', routes())
    fireEvent.click(await screen.findByRole('checkbox', { name: /mail-accounts/ }, slow))
    fireEvent.change(screen.getByRole('textbox', { name: /Comment/ }), { target: { value: 'New key' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add access key' }))
    await waitFor(() =>
      expect(app.calls.find((c) => c.key === 'POST /account/access-keys')?.body).toEqual({
        permissions: ['mail-accounts'],
        comment: 'New key',
      }),
      slow,
    )
    expect(await screen.findByText('Warning! Last chance to save secret key!', {}, slow)).toBeTruthy()
    // Hestia's message with the key ID in code (owner ruling 16)
    const id = (await screen.findAllByText(NEW_ID, {}, slow)).find((e) => e.tagName === 'CODE' && e.parentElement?.textContent?.includes('has been created successfully.'))
    expect(id?.parentElement?.textContent).toBe(`Access key ${NEW_ID} has been created successfully.`)
    expect(screen.getByText(SECRET)).toBeTruthy()
    expect(app.location()).toBe(`/account/access-keys/${NEW_ID}`)
    // The secret is not in the URL or in any later fetch.
    expect(app.calls.some((c) => c.key.includes('secret'))).toBe(false)
  })

  it('shows the API error text', async () => {
    renderApp(
      '/account/access-keys/new',
      keysRoutes(
        {},
        {
          'POST /account/access-keys': () => ({
            status: 400,
            body: { error: { code: 'invalid', message: 'API %s not allowed', args: ['billing'] } },
          }),
        },
      ),
    )
    fireEvent.click(await screen.findByRole('checkbox', { name: /billing/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Add access key' }))
    expect(await screen.findByText('API billing not allowed')).toBeTruthy()
  })

  it('says so when no API can be granted', async () => {
    renderApp('/account/access-keys/new', keysRoutes({}, { 'GET /account/access-keys': () => ({ body: { keys: [], apis: [] } }) }))
    expect(await screen.findByText('No APIs available')).toBeTruthy()
  })

  it('keeps the secret out of the mutation cache by calling the API directly', () => {
    expect(source).not.toMatch(/addAccessKey/)
  })
})

describe('empty list', () => {
  it('shows the empty state with a way to add a key', async () => {
    renderApp('/account/access-keys', keysRoutes({}, { 'GET /account/access-keys': () => ({ body: { keys: [], apis: [] } }) }))
    expect(await screen.findByText('No access keys yet')).toBeTruthy()
  })
})

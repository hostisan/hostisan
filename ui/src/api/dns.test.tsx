import { cleanup, fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { dnsRoutes, renderApp } from '../test/renderApp'
import { api, ApiError } from './client'
import { apiErrorText } from './errors'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const t = (s: string) => (s === 'Domain' ? 'Domaine' : s)

describe('apiErrorText', () => {
  it('fills %s with the translated args', () => {
    const err = new ApiError(400, 'invalid', 'Field "%s" can not be blank.', ['Domain', 'IP Address'])
    expect(apiErrorText(err, t)).toBe('Field "Domaine, IP Address" can not be blank.')
  })
  it('passes Hestia output through', () => {
    expect(apiErrorText(new ApiError(404, 'not_found', "Error: x doesn't exist"), t)).toBe("Error: x doesn't exist")
  })
  it('falls back by code when the message is empty', () => {
    expect(apiErrorText(new ApiError(504, 'timeout', ''), t)).toBe('The server took too long to respond. Try again.')
    expect(apiErrorText(new ApiError(500, 'server_error', ''), t)).toBe('Something went wrong. Try again.')
    expect(apiErrorText(new Error('boom'), t)).toBe('Something went wrong. Try again.')
  })
})

describe('api client verbs', () => {
  it('sends PUT and DELETE and keeps error args', async () => {
    const fetch = vi.fn(
      async () =>
        new Response(JSON.stringify({ error: { code: 'invalid', message: 'm %s', args: ['a'] } }), { status: 400 }),
    )
    vi.stubGlobal('fetch', fetch)
    await expect(api.put('/dns/x', { a: 1 })).rejects.toMatchObject({ args: ['a'], code: 'invalid' })
    await expect(api.del('/dns/x')).rejects.toBeInstanceOf(ApiError)
    const methods = (fetch.mock.calls as unknown as [string, RequestInit][]).map((c) => c[1].method)
    expect(methods).toEqual(['PUT', 'DELETE'])
  })
})

describe('DNS pages over the API', () => {
  it('records page for a suspended zone shows the banner', async () => {
    renderApp('/dns/example.org', dnsRoutes())
    expect(await screen.findByText('This DNS domain is suspended')).toBeTruthy()
  })

  it("unknown zone shows the doesn't-exist banner with Hestia's message", async () => {
    renderApp('/dns/missing.example', dnsRoutes())
    expect(await screen.findByText("DNS domain missing.example doesn't exist")).toBeTruthy()
    expect(await screen.findByText("Error: dns domain missing.example doesn't exist")).toBeTruthy()
  })

  it('offers a non-admin only Delete in records bulk', async () => {
    renderApp('/dns/example.com', dnsRoutes({ role: 'user', name: 'bob' }))
    await screen.findByText('www')
    fireEvent.click(screen.getAllByRole('checkbox')[1])
    expect(await screen.findByRole('button', { name: 'Delete' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Suspend' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Unsuspend' })).toBeNull()
  })

  it('offers an admin Suspend and Unsuspend in records bulk', async () => {
    renderApp('/dns/example.com', dnsRoutes())
    await screen.findByText('www')
    fireEvent.click(screen.getAllByRole('checkbox')[1])
    expect(await screen.findByRole('button', { name: 'Suspend' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Unsuspend' })).toBeTruthy()
  })

  it('DNSSEC page shows the DS record', async () => {
    renderApp('/dns/example.com/dnssec', dnsRoutes())
    expect(await screen.findByText('example.com. IN DS 12345 13 2 DEADBEEF')).toBeTruthy()
    expect(screen.getByText('13 - ECDSAP256/SHA256')).toBeTruthy()
  })
})

import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp, signedIn } from '../../test/renderApp'
import { serverRoutes } from '../../test/serverFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('white label page', () => {
  it('shows the fields with the server values and no reset-logo control', async () => {
    renderApp('/server/white-label', serverRoutes())
    expect((await screen.findByLabelText('Application name') as HTMLInputElement).value).toBe('Hostisan')
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('{{page}} - {{hostname}} - {{appname}}')
    expect(screen.getByLabelText('Sender name')).toBeTruthy()
    expect(screen.getByLabelText('Sender email address')).toBeTruthy()
    expect((screen.getByLabelText('Email subject') as HTMLInputElement).value).toBe('{{subject}}')
    expect(screen.getByText('Default: noreply@server1.acme-hosting.net')).toBeTruthy()
    expect(screen.getByRole('checkbox', { name: 'Update logo' })).toBeTruthy()
    expect(screen.queryByText(/Reset logo/)).toBeNull()
    expect(document.body.textContent).not.toMatch(/Hestia/)
  })

  it('saves the form and refreshes the bootstrap so the new name shows', async () => {
    let bootstrap = 0
    const { calls } = renderApp(
      '/server/white-label',
      serverRoutes({
        'GET /bootstrap': () => ({ body: { ...signedIn({}), appName: bootstrap++ === 0 ? 'Hostisan' : 'Acme Panel' } }),
        'PUT /server/white-label': () => ({ body: { status: 'ok' } }),
      }),
    )
    const name = (await screen.findByLabelText('Application name')) as HTMLInputElement
    fireEvent.change(name, { target: { value: 'Acme Panel' } })
    fireEvent.click(screen.getByRole('checkbox', { name: 'Update logo' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(calls.find((c) => c.key === 'PUT /server/white-label')?.body).toEqual({
        appName: 'Acme Panel',
        title: '{{page}} - {{hostname}} - {{appname}}',
        fromName: '',
        fromEmail: '',
        subjectEmail: '{{subject}}',
        hideDocs: false,
        updateLogo: true,
      }),
    )
    await waitFor(() => expect(calls.filter((c) => c.key === 'GET /bootstrap').length).toBeGreaterThan(1))
    expect(await screen.findByText('Changes have been saved.')).toBeTruthy() // Hestia's msgid (ruling 19)
  })

  it('shows a rejected save', async () => {
    renderApp(
      '/server/white-label',
      serverRoutes({
        'PUT /server/white-label': () => ({ status: 400, body: { error: { code: 'invalid', message: 'Field "%s" can not be blank.', args: ['Application name'] } } }),
      }),
    )
    fireEvent.change(await screen.findByLabelText('Application name'), { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Field "Application name" can not be blank.')).toBeTruthy()
  })
})

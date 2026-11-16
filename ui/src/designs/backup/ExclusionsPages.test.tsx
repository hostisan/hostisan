import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { backupRoutes } from '../../test/accountFixtures'
import { renderApp } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('backup exclusions', () => {
  it('lists every section with its entries', async () => {
    renderApp('/backup/exclusions', backupRoutes())
    expect(await screen.findByText('public_html/cache, public_html/tmp')).toBeTruthy()
    expect(screen.getByText('example.org')).toBeTruthy()
    expect(screen.getByText('Downloads')).toBeTruthy()
    expect(screen.getByRole('link', { name: /Edit backup exclusions/ })).toBeTruthy()
  })

  it('shows four textareas with the stored text and placeholders', async () => {
    renderApp('/backup/exclusions/edit', backupRoutes())
    const web = (await screen.findByRole('textbox', { name: 'Web domains' })) as HTMLTextAreaElement
    expect(web.value).toBe('example.com:public_html/cache:public_html/tmp\nshop.example.com')
    expect((screen.getByRole('textbox', { name: 'Mail domains' }) as HTMLTextAreaElement).value).toBe(
      'example.com:info:support',
    )
    expect((screen.getByRole('textbox', { name: 'Databases' }) as HTMLTextAreaElement).value).toBe('admin_wp')
    expect((screen.getByRole('textbox', { name: 'User directory' }) as HTMLTextAreaElement).value).toBe(
      'Downloads\n.cache',
    )
    expect(web.placeholder).toContain('To exclude all domains use *')
    expect(screen.getAllByRole('textbox')).toHaveLength(4)
  })

  it('saves the four texts', async () => {
    const { calls, location } = renderApp(
      '/backup/exclusions/edit',
      backupRoutes({}, { 'PUT /backup/exclusions': () => ({ body: { status: 'ok' } }) }),
    )
    const mail = await screen.findByRole('textbox', { name: 'Mail domains' })
    fireEvent.change(mail, { target: { value: 'example.com:info:sales' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(location()).toBe('/backup/exclusions'))
    expect(calls.find((c) => c.key === 'PUT /backup/exclusions')?.body).toEqual({
      web: 'example.com:public_html/cache:public_html/tmp\nshop.example.com',
      mail: 'example.com:info:sales',
      db: 'admin_wp',
      userdir: 'Downloads\n.cache',
    })
  })

  it('shows the server error and stays on the form', async () => {
    const { location } = renderApp(
      '/backup/exclusions/edit',
      backupRoutes(
        {},
        {
          'PUT /backup/exclusions': () => ({
            status: 400,
            body: { error: { code: 'invalid', message: 'Error: invalid exclusion entry :: ..' } },
          }),
        },
      ),
    )
    await screen.findByRole('textbox', { name: 'Mail domains' })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Error: invalid exclusion entry :: ..')).toBeTruthy()
    expect(location()).toBe('/backup/exclusions/edit')
  })
})

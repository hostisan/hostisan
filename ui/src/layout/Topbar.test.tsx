import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp } from '../test/renderApp'
import { toolsRoutes } from '../test/toolsFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

async function field() {
  return (await screen.findByRole('searchbox', { name: 'Search' })) as HTMLInputElement
}

// The backdrop is any page but the terminal: xterm's animation frames outlive a test
// under jsdom. The DNS list (unanswered here) keeps the blank-field check meaningful.
describe('top bar', () => {
  it('has a search field that opens the search page with the trimmed query', async () => {
    const { location, calls } = renderApp('/search', toolsRoutes())
    const input = await field()
    expect(input.type).toBe('search')
    expect(input.getAttribute('data-shortcut')).toBe('search')
    fireEvent.change(input, { target: { value: '  example ' } })
    fireEvent.submit(input.closest('form') as HTMLFormElement)
    await waitFor(() => expect(location()).toBe('/search'))
    await screen.findByRole('table')
    expect(calls.some((c) => c.key === 'GET /search?q=example')).toBe(true)
  })

  it('does nothing for a blank field', async () => {
    const { location } = renderApp('/dns', toolsRoutes())
    const input = await field()
    fireEvent.change(input, { target: { value: '   ' } })
    fireEvent.submit(input.closest('form') as HTMLFormElement)
    expect(location()).toBe('/dns')
  })

  it('keeps the palette button', async () => {
    renderApp('/search', toolsRoutes())
    await field()
    expect(screen.getByRole('button', { name: /^Search \(/ })).toBeTruthy()
  })

  it('has no Help item while Hostisan has no docs site', async () => {
    renderApp('/search', toolsRoutes())
    await field()
    fireEvent.click(screen.getByRole('button', { name: 'admin' }))
    await screen.findByRole('menuitem', { name: 'Edit account' })
    expect(screen.queryByRole('menuitem', { name: 'Help' })).toBeNull()
  })

  it('shows the query in the field on the results page', async () => {
    renderApp('/search?q=example', toolsRoutes())
    expect((await field()).value).toBe('example')
  })
})

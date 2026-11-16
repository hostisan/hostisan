import { cleanup, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { renderApp, signedIn } from '../test/renderApp'

const docs = vi.hoisted(() => ({ url: '' }))
vi.mock('./help', async (orig) => ({
  ...(await orig<typeof import('./help')>()),
  get docsUrl() {
    return docs.url
  },
}))

afterEach(() => {
  cleanup()
  docs.url = ''
})

const footer = () => screen.findByRole('navigation', { name: 'Footer' })

describe('footer documentation link', () => {
  it('is hidden while Hostisan has no docs site', async () => {
    renderApp('/web', { 'GET /bootstrap': () => ({ body: signedIn({}) }) })
    expect((await footer()).textContent).not.toContain('Documentation')
  })

  it('points at the docs site once it exists', async () => {
    docs.url = 'https://docs.example.test'
    renderApp('/web', { 'GET /bootstrap': () => ({ body: signedIn({}) }) })
    await footer()
    expect(screen.getByRole('link', { name: 'Documentation' }).getAttribute('href')).toBe('https://docs.example.test')
  })

  it('respects HIDE_DOCS', async () => {
    docs.url = 'https://docs.example.test'
    renderApp('/web', { 'GET /bootstrap': () => ({ body: { ...signedIn({}), hideDocs: true } }) })
    expect((await footer()).textContent).not.toContain('Documentation')
  })
})

describe('footer product links', () => {
  const product = { productUrl: 'https://example.test', securityEmail: 'security@example.test' }
  const href = (name: string) => screen.getByRole('link', { name }).getAttribute('href')

  it('come from the bootstrap (product.env)', async () => {
    renderApp('/web', { 'GET /bootstrap': () => ({ body: { ...signedIn({}), ...product } }) })
    await footer()
    expect(href('Report a security issue')).toBe('mailto:security@example.test')
    expect(href('Terms of use')).toBe('https://example.test/terms')
    expect(href('Privacy policy')).toBe('https://example.test/privacy')
  })

  it('are left out in a build without them', async () => {
    renderApp('/web', { 'GET /bootstrap': () => ({ body: { ...signedIn({}), productUrl: '', securityEmail: '' } }) })
    const text = (await footer()).textContent
    expect(text).not.toContain('Report a security issue')
    expect(text).not.toContain('Terms of use')
    expect(text).not.toContain('Privacy policy')
  })
})

describe('footer version', () => {
  const copyright = async () => (await footer()).parentElement?.textContent ?? ''

  it('adds a v to a bare version', async () => {
    renderApp('/web', { 'GET /bootstrap': () => ({ body: { ...signedIn({}), version: '1.0.0' } }) })
    expect(await copyright()).toContain('Hostisan · v1.0.0')
  })

  it('does not double the v of a build version', async () => {
    renderApp('/web', { 'GET /bootstrap': () => ({ body: { ...signedIn({}), version: 'v1.0.0-184-g8b7d203ad' } }) })
    const text = await copyright()
    expect(text).toContain('Hostisan · v1.0.0-184-g8b7d203ad')
    expect(text).not.toContain('vv')
  })
})

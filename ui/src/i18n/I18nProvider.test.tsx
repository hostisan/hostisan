import { LayerDialog } from '@cloudflare/kumo'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { anonymous, renderApp } from '../test/renderApp'
import { I18nProvider, useT, type Translate } from './I18nProvider'

function Probe() {
  const t = useT()
  return (
    <>
      <p>{t('Invalid username or password')}</p>
      <p>{t('Some string nobody translated')}</p>
      <p data-testid="brand">{t('Hestia Control Panel')}</p>
    </>
  )
}

function renderWith(lang: string, children: ReactNode = <Probe />, version?: string) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider lang={lang} version={version}>
        {children}
      </I18nProvider>
    </QueryClientProvider>,
  )
}

/** Hands the test the provider's t(). */
function Capture({ onT }: { onT: (t: Translate) => void }) {
  onT(useT())
  return null
}

function catalog(entries: Record<string, string>) {
  return vi.fn(async (_url: string) => new Response(JSON.stringify(entries)))
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('useT', () => {
  it('translates from the loaded catalog', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ 'Invalid username or password': "Nom d'utilisateur ou mot de passe invalide" }))))
    renderWith('fr')
    expect(await screen.findByText("Nom d'utilisateur ou mot de passe invalide")).toBeTruthy()
  })

  it('falls back to the msgid', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{}')))
    renderWith('fr')
    expect(await screen.findByText('Some string nobody translated')).toBeTruthy()
  })

  it('does not keep a failed catalogue: the next load fetches it again', async () => {
    let calls = 0
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        ++calls === 1
          ? new Response('{}', { status: 500 })
          : new Response(JSON.stringify({ 'Invalid username or password': 'Identifiants refusés' })),
      ),
    )
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const tree = (
      <QueryClientProvider client={client}>
        <I18nProvider lang="fr">
          <Probe />
        </I18nProvider>
      </QueryClientProvider>
    )
    const first = render(tree)
    // English meanwhile.
    expect(await screen.findByText('Invalid username or password')).toBeTruthy()
    await waitFor(() => expect(calls).toBe(1))
    await waitFor(() => expect(client.getQueryState(['i18n', 'fr', ''])?.status).not.toBe('pending'))
    first.unmount()
    render(tree)
    expect(await screen.findByText('Identifiants refusés')).toBeTruthy()
  })

  it('does not fetch a catalog for English', async () => {
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)
    renderWith('en')
    expect(await screen.findByText('Invalid username or password')).toBeTruthy()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('rewrites the brand in untranslated English msgids', async () => {
    vi.stubGlobal('fetch', vi.fn())
    renderWith('en')
    expect((await screen.findByTestId('brand')).textContent).toBe('Hostisan')
  })

  it('t fills %s and %d in order', () => {
    const got: { t?: Translate } = {}
    renderWith('en', <Capture onT={(t) => (got.t = t)} />)
    expect(got.t!('Domain {%s} has been created successfully.', 'example.com')).toBe(
      'Domain {example.com} has been created successfully.',
    )
    expect(got.t!('%s of %d used', 'web', 3)).toBe('web of 3 used')
  })

  it('t leaves the text when the arg count differs', () => {
    const got: { t?: Translate } = {}
    renderWith('en', <Capture onT={(t) => (got.t = t)} />)
    expect(got.t!('Invalid %s format :: %s', 'domain')).toBe('Invalid %s format :: %s')
    expect(got.t!('Changes have been saved.', 'extra')).toBe('Changes have been saved.')
    expect(got.t!('Invalid %s format :: %s')).toBe('Invalid %s format :: %s')
  })

  it('fills args into the translation, not the msgid', async () => {
    vi.stubGlobal('fetch', catalog({ 'Restored %s files': '%s fichiers restaurés' }))
    function Line() {
      return <p>{useT()('Restored %s files', 3)}</p>
    }
    renderWith('fr', <Line />)
    expect(await screen.findByText('3 fichiers restaurés')).toBeTruthy()
  })

  it('fetches the catalogue with the version', async () => {
    const app = renderApp('/login', {
      'GET /bootstrap': () => ({ body: { ...anonymous, language: 'fr' } }),
      'GET /i18n/fr?v=1.0.0': () => ({ body: { 'Sign in': 'Connexion' } }),
    })
    await waitFor(() => expect(app.calls.map((c) => c.key)).toContain('GET /i18n/fr?v=1.0.0'))
    expect(app.calls.map((c) => c.key).filter((k) => k.startsWith('GET /i18n/'))).toEqual(['GET /i18n/fr?v=1.0.0'])
  })

  it('sets the html lang', async () => {
    vi.stubGlobal('fetch', catalog({}))
    const view = renderWith('de')
    await waitFor(() => expect(document.documentElement.lang).toBe('de'))
    view.unmount()
    renderWith('en')
    await waitFor(() => expect(document.documentElement.lang).toBe('en'))
  })

  it("translates Kumo's own dialog labels", async () => {
    vi.stubGlobal('fetch', catalog({ Close: 'Fermer', Cancel: 'Annuler' }))
    renderWith(
      'fr',
      <LayerDialog.Root open>
        <LayerDialog.Content>
          <LayerDialog.Title>Titre</LayerDialog.Title>
          <LayerDialog.Body>Corps</LayerDialog.Body>
        </LayerDialog.Content>
      </LayerDialog.Root>,
    )
    expect(await screen.findByRole('button', { name: 'Fermer' })).toBeTruthy()
  })

  it("translates the confirm dialog's Cancel button", async () => {
    vi.stubGlobal('fetch', catalog({ Cancel: 'Annuler' }))
    renderWith(
      'fr',
      <ConfirmDialog open onOpenChange={() => {}} title="T" description="D" confirmLabel="OK" onConfirm={() => {}} />,
    )
    expect(await screen.findByRole('button', { name: 'Annuler' })).toBeTruthy()
  })
})

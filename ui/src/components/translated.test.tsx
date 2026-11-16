import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { I18nProvider, useT } from '../i18n/I18nProvider'
import { DeleteResource } from './DeleteResource'
import { UsageMeter } from './PageAside'
import { PasswordInput } from './PasswordInput'
import { StatusBadge } from './StatusBadge'

const fr: Record<string, string> = {
  'web domain': 'domaine web',
  'Delete %s': 'Supprimer %s',
  'This action cannot be undone. This will permanently delete the %s %s.':
    'Cette action est irréversible. Le %2$s %1$s sera supprimé définitivement.'.replace('%2$s %1$s', '%s %s'),
  'Type %s to confirm:': 'Tapez %s pour confirmer :',
  'Type %s to confirm deletion': 'Tapez %s pour confirmer la suppression',
  Cancel: 'Annuler',
  Strength: 'Robustesse',
  '1 number': '1 chiffre',
  'Generate password': 'Générer un mot de passe',
  'Show password': 'Afficher le mot de passe',
  Active: 'Actif',
  Unlimited: 'Illimité',
}

beforeEach(() => vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(fr)))))
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function french(children: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider lang="fr" version="1.0.0">
        {children}
      </I18nProvider>
    </QueryClientProvider>,
  )
}

function Delete() {
  const t = useT()
  return (
    <DeleteResource open onOpenChange={() => {}} resourceType={t('web domain')} resourceName="example.com" onDelete={() => {}} />
  )
}

describe('shared components in French', () => {
  it('the delete dialog puts the translated noun into whole sentences', async () => {
    french(<Delete />)
    expect(await screen.findByRole('button', { name: 'Supprimer domaine web' })).toBeTruthy()
    expect(screen.getByText('Supprimer example.com')).toBeTruthy()
    expect(screen.getByText(/Cette action est irréversible\. Le/).textContent).toBe(
      'Cette action est irréversible. Le example.com domaine web sera supprimé définitivement.',
    )
    expect(screen.getByText(/Tapez/).textContent).toBe('Tapez example.com pour confirmer :')
    expect(screen.getByLabelText('Tapez example.com pour confirmer la suppression')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Annuler' })).toBeTruthy()
  })

  it('the password field speaks French', async () => {
    french(<PasswordInput label="Mot de passe" />)
    expect(await screen.findByText('Robustesse')).toBeTruthy()
    expect(screen.getByText('1 chiffre')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Générer un mot de passe' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Afficher le mot de passe' })).toBeTruthy()
  })

  it('badges and meters speak French', async () => {
    french(
      <>
        <StatusBadge suspended={false} />
        <UsageMeter label="Disque" used={3} limit="unlimited" />
      </>,
    )
    expect(await screen.findByText('Actif')).toBeTruthy()
    expect(screen.getByText('3 / Illimité')).toBeTruthy()
  })
})

describe('the delete dialog in English', () => {
  it('keeps its sentences', () => {
    vi.stubGlobal('fetch', vi.fn())
    render(
      <QueryClientProvider client={new QueryClient()}>
        <I18nProvider lang="en">
          <DeleteResource open onOpenChange={() => {}} resourceType="web domain" resourceName="example.com" onDelete={() => {}} />
        </I18nProvider>
      </QueryClientProvider>,
    )
    expect(screen.getByText(/This action cannot be undone/).textContent).toBe(
      'This action cannot be undone. This will permanently delete the example.com web domain.',
    )
    expect(screen.getByRole('button', { name: 'Delete web domain' })).toBeTruthy()
  })
})

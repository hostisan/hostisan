import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { I18nProvider } from '../../i18n/I18nProvider'
import { ComboboxField } from './ComboboxField'
import { DateField } from './DateField'
import { ALL_DATES, DateRangeFilter } from './DateRangeFilter'
import { NameServersField } from './NameServersField'
import { Optional } from './Optional'

const fr: Record<string, string> = {
  Search: 'Rechercher',
  'No matches': 'Aucun résultat',
  'Pick a date': 'Choisir une date',
  Done: 'Terminé',
  'All dates': 'Toutes les dates',
  'From %s': 'Depuis le %s',
  'Until %s': "Jusqu'au %s",
  'Date range: %s': 'Période : %s',
  Jan: 'janv.',
  Mar: 'mars',
  'Name servers': 'Serveurs de noms',
  'Between %d and %d host names. Press Enter or comma after each one.':
    'Entre %d et %d noms d’hôte. Appuyez sur Entrée ou virgule après chacun.',
  'Add at least %d name servers.': 'Ajoutez au moins %d serveurs de noms.',
  'Up to %d name servers are allowed.': "Jusqu'à %d serveurs de noms sont autorisés.",
  optional: 'facultatif',
}

function renderFr(children: ReactNode) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response(JSON.stringify(fr))),
  )
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider lang="fr">{children}</I18nProvider>
    </QueryClientProvider>,
  )
}

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe('field strings are translated', () => {
  it('ComboboxField placeholder', async () => {
    renderFr(<ComboboxField label="Fuseau" options={[{ value: 'UTC', label: 'UTC' }]} />)
    expect(await screen.findByPlaceholderText('Rechercher')).toBeTruthy()
  })

  it('DateField empty button', async () => {
    renderFr(<DateField label="Expire" value="" onChange={() => {}} />)
    expect(await screen.findByRole('button', { name: 'Choisir une date' })).toBeTruthy()
  })

  it('DateRangeFilter labels and months', async () => {
    renderFr(<DateRangeFilter value={ALL_DATES} onChange={() => {}} toolbar={false} />)
    expect(await screen.findByRole('button', { name: 'Période : Toutes les dates' })).toBeTruthy()
    cleanup()

    const range = { start: new Date(2026, 0, 5), end: null }
    renderFr(<DateRangeFilter value={range} onChange={() => {}} toolbar={false} />)
    expect(await screen.findByRole('button', { name: 'Période : Depuis le janv. 5' })).toBeTruthy()
    cleanup()

    renderFr(
      <DateRangeFilter
        value={{ start: null, end: new Date(2026, 2, 1) }}
        onChange={() => {}}
        granularity="month"
        toolbar={false}
      />,
    )
    expect(await screen.findByRole('button', { name: "Période : Jusqu'au mars 2026" })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /Période/ }))
    expect(await screen.findByRole('button', { name: 'Terminé' })).toBeTruthy()
  })

  it('NameServersField label, help and limits', async () => {
    renderFr(<NameServersField defaultValue={['ns1.example.net', 'ns2.example.net']} />)
    expect(await screen.findByText('Serveurs de noms')).toBeTruthy()
    expect(screen.getByText('Entre 2 et 8 noms d’hôte. Appuyez sur Entrée ou virgule après chacun.')).toBeTruthy()
    cleanup()

    renderFr(<NameServersField defaultValue={['ns1.example.net']} />)
    expect(await screen.findByText('Ajoutez au moins 2 serveurs de noms.')).toBeTruthy()
  })

  it('Optional marker', async () => {
    renderFr(<Optional />)
    expect(await screen.findByText('(facultatif)')).toBeTruthy()
  })
})

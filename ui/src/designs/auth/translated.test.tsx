import { cleanup, fireEvent, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { anonymous, renderApp } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const fr: Record<string, string> = {
  'Welcome to %s': 'Bienvenue sur %s',
  'Welcome %s!': 'Bienvenue %s !',
  'Log in to manage your websites, mail and databases.': 'Connectez-vous pour gérer vos sites, e-mails et bases de données.',
  Username: "Nom d'utilisateur",
  Password: 'Mot de passe',
  Next: 'Suivant',
  'Log in': 'Se connecter',
  Back: 'Retour',
  Change: 'Modifier',
  Language: 'Langue',
}

const routes = (language = 'en') => ({
  'GET /bootstrap': () => ({ body: { ...anonymous, language } }),
  'GET /i18n/fr?v=1.0.0': () => ({ body: fr }),
})

describe('sign-in screens in French', () => {
  it('Login page labels follow the picked language', async () => {
    renderApp('/login', routes())
    expect(await screen.findByText('Welcome to Hostisan')).toBeTruthy()
    fireEvent.click(screen.getByRole('combobox', { name: 'Language' }))
    const opt = await screen.findByRole('option', { name: 'Français' })
    fireEvent.pointerDown(opt)
    fireEvent.mouseDown(opt)
    fireEvent.pointerUp(opt)
    fireEvent.mouseUp(opt)
    fireEvent.click(opt)
    expect(await screen.findByText('Bienvenue sur Hostisan')).toBeTruthy()
    expect(screen.getByText('Connectez-vous pour gérer vos sites, e-mails et bases de données.')).toBeTruthy()
    expect(screen.getByLabelText("Nom d'utilisateur")).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Suivant' })).toBeTruthy()
    expect(screen.getByRole('combobox', { name: 'Langue' })).toBeTruthy()
  })

  it('the password step greets the user in French', async () => {
    renderApp('/login', routes('fr'))
    const input = await screen.findByLabelText("Nom d'utilisateur")
    fireEvent.change(input, { target: { value: 'bob' } })
    fireEvent.submit(input.closest('form')!)
    expect(await screen.findByText('Bienvenue bob !')).toBeTruthy()
    expect(screen.getByText('Mot de passe')).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Se connecter' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Retour' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Modifier' })).toBeTruthy()
  })
})

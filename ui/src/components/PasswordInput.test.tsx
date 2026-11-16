import { cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PasswordInput } from './PasswordInput'

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('PasswordInput', () => {
  it('renders without Kumo warnings and is named by its label', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    render(<PasswordInput label="New password" defaultValue="Secret1x" />)
    expect(screen.getByLabelText('New password')).toBeTruthy()
    expect(warn).not.toHaveBeenCalled()
  })

  it('is named by the page label when the page renders its own', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    render(
      <>
        <label id="password-label" htmlFor="password">
          Password
        </label>
        <PasswordInput id="password" label={null} isNew={false} />
      </>,
    )
    expect(screen.getByLabelText('Password')).toBeTruthy()
    expect(warn).not.toHaveBeenCalled()
  })
})

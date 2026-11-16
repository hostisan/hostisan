import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { NameServersField } from './NameServersField'

afterEach(cleanup)

describe('NameServersField', () => {
  it('stops at eight name servers with a Hostisan-neutral message', async () => {
    const eight = Array.from({ length: 8 }, (_, i) => `ns${i + 1}.example.net`)
    render(<NameServersField defaultValue={eight} />)
    const input = screen.getByRole('textbox')
    fireEvent.change(input, { target: { value: 'ns9.example.net' } })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(await screen.findByText('Up to 8 name servers are allowed.')).toBeTruthy()
    expect(screen.queryByText(/hestia/i)).toBeNull()
    expect(document.body.textContent).not.toMatch(/hestia/i)
  })
})

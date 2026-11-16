import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, describe, expect, it } from 'vitest'
import { ComboboxField } from './ComboboxField'

afterEach(cleanup)

const options = [
  { value: 'UTC', label: 'UTC' },
  { value: 'Europe/Paris', label: 'Europe Paris' },
]

function Controlled() {
  const [tz, setTz] = useState('UTC')
  return (
    <>
      <ComboboxField label="Time zone" options={options} value={tz} onChange={setTz} />
      <button type="button" onClick={() => setTz('Europe/Paris')}>
        Paris
      </button>
      <output>{tz}</output>
    </>
  )
}

describe('ComboboxField', () => {
  it('follows a controlled value', () => {
    render(<Controlled />)
    const input = screen.getByRole('combobox', { name: 'Time zone' }) as HTMLInputElement
    expect(input.value).toBe('UTC')
    fireEvent.click(screen.getByRole('button', { name: 'Paris' }))
    expect(input.value).toBe('Europe Paris')
  })

  it('keeps its own value and submits it when uncontrolled', () => {
    const { container } = render(<ComboboxField label="Language" name="lang" options={options} defaultValue="Europe/Paris" />)
    expect((container.querySelector('input[name="lang"]') as HTMLInputElement).value).toBe('Europe/Paris')
  })
})

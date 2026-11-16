import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { FormGuardContext, type FormGuard } from '../../unsaved/types'
import { ComboboxField } from './ComboboxField'
import { DateField } from './DateField'
import { NameServersField } from './NameServersField'
import { SelectField } from './SelectField'
import { TagField } from './TagField'

afterEach(cleanup)

function withGuard(children: ReactNode) {
  const guard: FormGuard = { dirty: false, markDirty: vi.fn(), release: vi.fn(), rearm: vi.fn(), reset: vi.fn() }
  render(<FormGuardContext.Provider value={guard}>{children}</FormGuardContext.Provider>)
  return guard
}

function choose(option: HTMLElement) {
  fireEvent.pointerDown(option)
  fireEvent.mouseDown(option)
  fireEvent.pointerUp(option)
  fireEvent.mouseUp(option)
  fireEvent.click(option)
}

const ab = [
  { value: 'a', label: 'Alpha' },
  { value: 'b', label: 'Beta' },
]

describe('shared fields mark the form changed', () => {
  it('SelectField', async () => {
    const guard = withGuard(<SelectField label="Kind" value="a" onChange={() => {}} options={ab} />)
    fireEvent.click(screen.getByRole('combobox', { name: 'Kind' }))
    choose(await screen.findByRole('option', { name: 'Beta' }))
    await waitFor(() => expect(guard.markDirty).toHaveBeenCalled())
  })

  it('ComboboxField', async () => {
    const guard = withGuard(<ComboboxField label="Kind" options={ab} defaultValue="a" />)
    const input = screen.getByRole('combobox', { name: 'Kind' })
    fireEvent.click(input)
    fireEvent.keyDown(input, { key: 'ArrowDown' })
    choose(await screen.findByRole('option', { name: 'Beta' }))
    await waitFor(() => expect(guard.markDirty).toHaveBeenCalled())
  })

  it('DateField', async () => {
    const guard = withGuard(<DateField label="Expires" value="2026-01-15" onChange={() => {}} />)
    fireEvent.click(screen.getByRole('button', { name: '2026-01-15' }))
    fireEvent.click(await screen.findByRole('button', { name: /January 20th/ }))
    expect(guard.markDirty).toHaveBeenCalled()
  })

  it('NameServersField', () => {
    const guard = withGuard(
      <NameServersField defaultValue={['ns1.example.net', 'ns2.example.net', 'ns3.example.net']} />,
    )
    fireEvent.click(screen.getByRole('button', { name: /Remove ns3\.example\.net/i }))
    expect(guard.markDirty).toHaveBeenCalled()
  })

  it('TagField', () => {
    const onValueChange = vi.fn()
    const guard = withGuard(<TagField label="Aliases" value={['www.example.net']} onValueChange={onValueChange} />)
    fireEvent.click(screen.getByRole('button', { name: /Remove www\.example\.net/i }))
    expect(guard.markDirty).toHaveBeenCalled()
    expect(onValueChange).toHaveBeenCalledWith([])
  })

  it('works outside a form page', () => {
    render(<SelectField label="Kind" value="a" onChange={() => {}} options={ab} />)
    expect(screen.getByRole('combobox', { name: 'Kind' })).toBeTruthy()
  })
})

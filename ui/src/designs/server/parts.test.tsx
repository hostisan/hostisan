import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ConfigEditor, Fact, RestartCheckbox, ToggleField } from './parts'

afterEach(cleanup)

/** Checked state of a native or ARIA checkbox/switch. */
const isOn = (el: HTMLElement) =>
  el.getAttribute('aria-checked') === 'true' || (el as HTMLInputElement).checked === true

describe('ConfigEditor', () => {
  it('is labelled with the path and reports edits when controlled', () => {
    const onChange = vi.fn()
    render(<ConfigEditor path="/etc/nginx/nginx.conf" value="worker_processes auto;" onChange={onChange} />)
    const box = screen.getByLabelText('/etc/nginx/nginx.conf')
    expect((box as HTMLTextAreaElement).value).toBe('worker_processes auto;')
    fireEvent.change(box, { target: { value: 'worker_processes 2;' } })
    expect(onChange).toHaveBeenCalledWith('worker_processes 2;')
  })

  it('keeps its own text when uncontrolled (the design use)', () => {
    render(<ConfigEditor path="/etc/crontab" value="a" />)
    const box = screen.getByLabelText('/etc/crontab')
    fireEvent.change(box, { target: { value: 'b' } })
    expect((box as HTMLTextAreaElement).value).toBe('b')
  })

  it('shows a read error instead of the text, and can be read-only', () => {
    render(<ConfigEditor path="/etc/a.conf" value="" error="Error: file doesn't exist" />)
    expect(screen.getByText("Error: file doesn't exist")).toBeTruthy()
    expect(screen.queryByLabelText('/etc/a.conf')).toBeNull()
    cleanup()
    render(<ConfigEditor path="/etc/crontab" value="x" readOnly />)
    expect(screen.getByLabelText('/etc/crontab').hasAttribute('readonly')).toBe(true)
  })
})

describe('RestartCheckbox', () => {
  it('is checked by default and can be controlled', () => {
    function Controlled() {
      const [on, setOn] = useState(false)
      return (
        <>
          <RestartCheckbox checked={on} onCheckedChange={setOn} />
          <span>{on ? 'on' : 'off'}</span>
        </>
      )
    }
    render(<RestartCheckbox />)
    expect(isOn(screen.getByRole('checkbox', { name: 'Restart the service after saving' }))).toBe(true)
    cleanup()
    render(<Controlled />)
    const box = screen.getByRole('checkbox', { name: 'Restart the service after saving' })
    expect(isOn(box)).toBe(false)
    fireEvent.click(box)
    expect(screen.getByText('on')).toBeTruthy()
  })
})

describe('ToggleField', () => {
  it('works uncontrolled and controlled', () => {
    const onChange = vi.fn()
    render(<ToggleField label="Debug mode" defaultChecked={false} />)
    const sw = screen.getByRole('switch', { name: 'Debug mode' })
    fireEvent.click(sw)
    expect(isOn(sw)).toBe(true)
    cleanup()
    render(<ToggleField label="Firewall" checked onCheckedChange={onChange} disabled={false} />)
    fireEvent.click(screen.getByRole('switch', { name: 'Firewall' }))
    expect(onChange).toHaveBeenCalledWith(false)
  })
})

describe('Fact', () => {
  it('shows a label and value', () => {
    render(<Fact label="Web server">apache2</Fact>)
    expect(screen.getByText('Web server')).toBeTruthy()
    expect(screen.getByText('apache2')).toBeTruthy()
  })
})

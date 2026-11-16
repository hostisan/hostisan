import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { cronRoutes } from '../../test/accountFixtures'
import { renderApp } from '../../test/renderApp'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const box = (name: string) => screen.findByRole('textbox', { name }) as Promise<HTMLInputElement>
const values = async () =>
  Promise.all(['Minute', 'Hour', 'Day', 'Month', 'Day of week'].map(async (n) => (await box(n)).value))

async function pick(label: RegExp | string, option: string) {
  fireEvent.click(await screen.findByRole('combobox', { name: label }))
  const opt = await screen.findByRole('option', { name: option })
  fireEvent.pointerDown(opt)
  fireEvent.mouseDown(opt)
  fireEvent.pointerUp(opt)
  fireEvent.mouseUp(opt)
  fireEvent.click(opt)
  await waitFor(() => expect(screen.queryByRole('option')).toBeNull())
}

const generate = () => fireEvent.click(screen.getByRole('button', { name: 'Generate' }))
const tab = async (name: string) => fireEvent.click(await screen.findByRole('tab', { name }))

describe('system time line', () => {
  it('shows the server time in its time zone', async () => {
    renderApp('/cron/new', cronRoutes())
    const line = await screen.findByText(/System time:/)
    await waitFor(() => expect(line.textContent).toMatch(/21 Oct 2026,? 12:00/))
    expect(line.textContent).toContain('UTC')
  })
})

describe('schedule generator (cron checklist 2.3)', () => {
  it('Minutes tab gives * * * * * by default and */5 when chosen', async () => {
    renderApp('/cron/new', cronRoutes())
    await box('Minute')
    fireEvent.change(await box('Minute'), { target: { value: '9' } })
    generate()
    expect(await values()).toEqual(['*', '*', '*', '*', '*'])
    await pick('Run command', 'Every 5 minutes')
    generate()
    expect(await values()).toEqual(['*/5', '*', '*', '*', '*'])
  })

  it('Hourly tab gives 0 * * * *', async () => {
    renderApp('/cron/new', cronRoutes())
    await tab('Hourly')
    generate()
    expect(await values()).toEqual(['0', '*', '*', '*', '*'])
    await pick('Run command', 'Every 6 hours')
    await pick('Minute', '45')
    generate()
    expect(await values()).toEqual(['45', '*/6', '*', '*', '*'])
  })

  it('Daily tab gives 0 12 * * * and offers the odd and even day presets', async () => {
    renderApp('/cron/new', cronRoutes())
    await tab('Daily')
    generate()
    expect(await values()).toEqual(['0', '12', '*', '*', '*'])
    fireEvent.click(await screen.findByRole('combobox', { name: 'Run command' }))
    expect((await screen.findAllByRole('option')).map((o) => o.textContent)).toEqual([
      'Every day', 'Every odd day', 'Every even day', 'Every 3 days', 'Every 5 days', 'Every 10 days', 'Every 15 days',
    ])
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('option')).toBeNull())
    await pick('Run command', 'Every odd day')
    generate()
    expect((await values())[2]).toBe('1-31/2')
  })

  it('Weekly tab gives 0 12 * * * and weekday and weekend lists', async () => {
    renderApp('/cron/new', cronRoutes())
    await tab('Weekly')
    generate()
    expect(await values()).toEqual(['0', '12', '*', '*', '*'])
    await pick('Run command', 'Weekdays (5 days)')
    generate()
    expect((await values())[4]).toBe('1,2,3,4,5')
    await pick('Run command', 'Weekend (2 days)')
    generate()
    expect((await values())[4]).toBe('0,6')
    await pick('Run command', 'Sunday')
    generate()
    expect((await values())[4]).toBe('0')
  })

  it('Monthly tab gives 0 12 1 * * and the odd month preset', async () => {
    renderApp('/cron/new', cronRoutes())
    await tab('Monthly')
    generate()
    expect(await values()).toEqual(['0', '12', '1', '*', '*'])
    await pick('Run command', 'Every odd month')
    generate()
    expect((await values())[3]).toBe('1-11/2')
    await pick('Run command', 'Dec')
    generate()
    expect((await values())[3]).toBe('12')
  })
})

describe('add cron job', () => {
  it('posts the six fields and returns to the list', async () => {
    const routes = cronRoutes({}, { 'POST /cron': () => ({ body: { status: 'ok' } }) })
    const { calls, location } = renderApp('/cron/new', routes)
    fireEvent.change(await screen.findByLabelText('Command'), { target: { value: `echo 'a' \; ls` } })
    fireEvent.change(await box('Minute'), { target: { value: '*/5' } })
    fireEvent.change(await box('Day of week'), { target: { value: '0,6' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add cron job' }))
    await waitFor(() => expect(calls.find((c) => c.key === 'POST /cron')).toBeTruthy())
    expect(calls.find((c) => c.key === 'POST /cron')?.body).toEqual({
      min: '*/5', hour: '*', day: '*', month: '*', wday: '0,6', cmd: `echo 'a' \; ls`,
    })
    await waitFor(() => expect(location()).toBe('/cron'))
  })

  it('shows the blank-field error with every missing name', async () => {
    const routes = cronRoutes({}, {
      'POST /cron': () => ({
        status: 400,
        body: { error: { code: 'invalid', message: 'Field "%s" can not be blank.', args: ['Minute', 'Command'] } },
      }),
    })
    renderApp('/cron/new', routes)
    fireEvent.click(await screen.findByRole('button', { name: 'Add cron job' }))
    expect(await screen.findByText('Field "Minute, Command" can not be blank.')).toBeTruthy()
  })

  it('puts an invalid schedule field error on that field', async () => {
    const routes = cronRoutes({}, {
      'POST /cron': () => ({
        status: 400,
        body: { error: { code: 'invalid', message: 'invalid %s format :: %s', args: ['hour', '5-1'] } },
      }),
    })
    renderApp('/cron/new', routes)
    fireEvent.change(await box('Hour'), { target: { value: '5-1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add cron job' }))
    const err = await screen.findByText('invalid hour format :: 5-1')
    expect(err).toBeTruthy()
    // the message sits with the Hour field, not in a banner
    const hour = screen.getByRole('textbox', { name: 'Hour' })
    expect(hour.closest('div.min-w-0')?.contains(err)).toBe(true)
    expect(screen.getByRole('textbox', { name: 'Minute' }).closest('div.min-w-0')?.textContent).not.toContain('invalid')
  })

  it('shows a command error in a banner and keeps the form', async () => {
    const routes = cronRoutes({}, {
      'POST /cron': () => ({ status: 400, body: { error: { code: 'invalid', message: 'Invalid cron command format' } } }),
    })
    const { location } = renderApp('/cron/new', routes)
    fireEvent.change(await screen.findByLabelText('Command'), { target: { value: 'a`b`' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add cron job' }))
    expect(await screen.findByText('Invalid cron command format')).toBeTruthy()
    expect(location()).toBe('/cron/new')
  })

  it('disables saving when read-only', async () => {
    renderApp('/cron/new', cronRoutes({ readOnly: true }))
    expect((await screen.findByRole('button', { name: 'Add cron job' })).hasAttribute('disabled')).toBe(true)
  })
})

describe('edit cron job', () => {
  it('loads the job and puts the changes', async () => {
    const routes = cronRoutes({}, { 'PUT /cron/1': () => ({ body: { status: 'ok' } }) })
    const { calls } = renderApp('/cron/1', routes)
    expect((await box('Minute')).value).toBe('*/15')
    expect((await screen.findByLabelText('Command') as HTMLInputElement).value).toMatch(/^php \/home\/admin/)
    fireEvent.change(await box('Minute'), { target: { value: '5' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(calls.find((c) => c.key === 'PUT /cron/1')).toBeTruthy())
    expect(calls.find((c) => c.key === 'PUT /cron/1')?.body).toMatchObject({ min: '5', hour: '*', wday: '*' })
  })

  it('is read-only for a suspended job', async () => {
    renderApp('/cron/2', cronRoutes())
    expect(await screen.findByText('This cron job is suspended')).toBeTruthy()
    expect((await box('Minute')).hasAttribute('disabled')).toBe(true)
    expect(screen.getByRole('button', { name: 'Save' }).hasAttribute('disabled')).toBe(true)
    expect(screen.queryByRole('button', { name: 'Generate' })).toBeNull()
  })

  it('says when the job does not exist', async () => {
    renderApp('/cron/99', cronRoutes())
    expect(await screen.findByText("Error: cron job 99 doesn't exist")).toBeTruthy()
    expect(screen.getAllByRole('link', { name: /Cron jobs/ }).length).toBeGreaterThan(0)
  })
})

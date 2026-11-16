import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { JobStatus } from '../../api/types.gen'
import { JOB_ID, jobDone, jobFailed, jobRunning } from '../../test/serverFixtures'
import { JobProgress } from './JobProgress'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

function setup(states: JobStatus[], status = 200) {
  const fetch = vi.fn(async (_url: string) => {
    const body = states.length > 1 ? states.shift() : states[0]
    return new Response(JSON.stringify(body), { status })
  })
  vi.stubGlobal('fetch', fetch)
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const invalidate = vi.spyOn(client, 'invalidateQueries')
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  return { fetch, invalidate, wrapper }
}

describe('JobProgress', () => {
  it('titles the job with a Kumo heading and no deprecation warning', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { wrapper } = setup([{ ...jobDone({ status: 'ok' }, ['Restart nginx']), title: 'Restart nginx' }])
    render(<JobProgress jobId={JOB_ID} />, { wrapper })
    expect(await screen.findByRole('heading', { level: 3, name: 'Restart nginx' })).toBeTruthy()
    expect(warn.mock.calls.flat().join(' ')).not.toContain('deprecated')
    warn.mockRestore()
  })

  it('shows the title and the steps while running, then reports done once', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const done = jobDone({ applied: ['Hostname'] }, ['Change hostname', 'Restart web server'])
    const { fetch, invalidate, wrapper } = setup([
      { ...jobRunning('Restart web server', ['Change hostname', 'Restart web server']), title: 'Save settings' },
      done,
    ])
    const onDone = vi.fn()
    render(<JobProgress jobId={JOB_ID} onDone={onDone} />, { wrapper })
    expect(await screen.findByText('Save settings')).toBeTruthy()
    expect(screen.getAllByRole('listitem').map((li) => li.textContent)).toEqual([
      'Change hostname',
      'Restart web server',
    ])
    expect(
      screen
        .getAllByRole('status')
        .map((s) => s.textContent)
        .join(' '),
    ).toContain('Restart web server')
    expect(fetch.mock.calls[0][0]).toBe(`/api/v1/server/jobs/${JOB_ID}`)
    await vi.advanceTimersByTimeAsync(1000)
    await waitFor(() => expect(onDone).toHaveBeenCalledTimes(1))
    expect(onDone.mock.calls[0][0]).toMatchObject({ state: 'done', result: { applied: ['Hostname'] } })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['server'] })
    await vi.advanceTimersByTimeAsync(3000)
    expect(onDone).toHaveBeenCalledTimes(1)
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('shows the failure in an error banner with its args filled', async () => {
    const { wrapper } = setup([
      jobFailed('Field "%s" can not be blank.', ['Validate', 'Add backup host'], 'invalid', ['Password']),
    ])
    const onDone = vi.fn()
    render(<JobProgress jobId={JOB_ID} onDone={onDone} />, { wrapper })
    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('Field "Password" can not be blank.')
    expect(screen.getAllByRole('listitem').map((li) => li.textContent)).toEqual(['Validate', 'Add backup host'])
    await waitFor(() => expect(onDone).toHaveBeenCalledTimes(1))
    expect(onDone.mock.calls[0][0]).toMatchObject({ state: 'failed' })
  })

  it('shows a timeout as Hestia sent it', async () => {
    const { wrapper } = setup([jobFailed('Error: command timed out', ['Restart nginx'], 'timeout')])
    render(<JobProgress jobId={JOB_ID} />, { wrapper })
    expect((await screen.findByRole('alert')).textContent).toContain('Error: command timed out')
  })

  it('reports a job that is gone', async () => {
    const { wrapper } = setup([{ error: { code: 'not_found', message: '' } } as unknown as JobStatus], 404)
    render(<JobProgress jobId={JOB_ID} />, { wrapper })
    expect((await screen.findByRole('alert')).textContent).toContain('Something went wrong. Try again.')
  })

  it('renders nothing without a job', () => {
    const { fetch, wrapper } = setup([jobDone()])
    const { container } = render(<JobProgress jobId="" />, { wrapper })
    expect(container.textContent).toBe('')
    expect(fetch).not.toHaveBeenCalled()
  })
})

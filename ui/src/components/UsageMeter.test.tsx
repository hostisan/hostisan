import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { UsageMeter } from './PageAside'

describe('UsageMeter', () => {
  it('shows a limit of 0 as 0, not Unlimited (Hestia: none allowed)', () => {
    render(<UsageMeter label="Cron jobs" used={0} limit="0" />)
    expect(screen.getByText('0 / 0')).toBeTruthy()
    expect(screen.queryByText(/Unlimited/)).toBeNull()
  })
  it('shows unlimited as Unlimited', () => {
    render(<UsageMeter label="Cron jobs" used={3} limit="unlimited" />)
    expect(screen.getByText('3 / Unlimited')).toBeTruthy()
  })
})

import { getConfig } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

describe('test setup', () => {
  it('gives findBy and waitFor 3s, so a loaded machine does not time them out', () => {
    expect(getConfig().asyncUtilTimeout).toBe(3000)
  })

  it('stubs the browser APIs Kumo tabs and the on-this-page nav need', () => {
    expect(typeof window.IntersectionObserver).toBe('function')
    const io = new IntersectionObserver(() => {})
    expect(io.takeRecords()).toEqual([])
    expect(() => document.createElement('div').scrollIntoView()).not.toThrow()
  })
})

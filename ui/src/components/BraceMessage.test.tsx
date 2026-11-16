import { cleanup, render } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { braceMessage } from './BraceMessage'

afterEach(cleanup)

describe('braceMessage', () => {
  it('puts the value in literally, even with $ patterns', () => {
    const value = "a$&b$'c$`d"
    const { container } = render(
      <p>{braceMessage('Package {%s} saved as %s.', value, (inner) => <b>{inner}</b>)}</p>,
    )
    expect(container.querySelector('b')?.textContent).toBe(value)
    expect(container.textContent).toBe(`Package ${value} saved as ${value}.`)
  })
})

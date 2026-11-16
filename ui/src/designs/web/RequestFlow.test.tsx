import { act, cleanup, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RequestFlow } from './RequestFlow'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

const base = {
  domain: 'example.com',
  ssl: true,
  letsEncrypt: true,
  proxy: true,
  proxyTemplate: 'caching',
  fastcgiCache: false,
  template: 'default',
  backend: 'PHP-8_2',
  webSystem: 'apache2',
  webBackend: 'php-fpm',
  proxySystem: 'nginx',
}

describe('request flow', () => {
  it('draws visitor, proxy, web server and PHP from the passed system', () => {
    render(<RequestFlow {...base} />)
    expect(screen.getAllByText('NGINX').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Apache').length).toBeGreaterThan(0)
    expect(screen.getAllByText('PHP-FPM').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Cache').length).toBeGreaterThan(0)
  })

  it('omits the proxy and PHP nodes the server does not have', () => {
    render(<RequestFlow {...base} proxySystem="" webBackend="" />)
    expect(screen.queryByText('NGINX')).toBeNull()
    expect(screen.queryByText('PHP-FPM')).toBeNull()
  })
})

// Owner ruling 22: horizontal when it fits, stacked when it doesn't; never clipped or scrolled.
describe('request flow layout', () => {
  function sized(width: { client: number; scroll: number }) {
    vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(() => width.client)
    vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockImplementation(() => width.scroll)
    const observers: (() => void)[] = []
    vi.stubGlobal(
      'ResizeObserver',
      class {
        constructor(cb: () => void) {
          observers.push(cb)
        }
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    )
    return () => act(() => observers.forEach((cb) => cb()))
  }
  const layout = () => screen.getByTestId('request-flow').getAttribute('data-orientation')

  it('stays horizontal when it fits', () => {
    sized({ client: 900, scroll: 700 })
    render(<RequestFlow {...base} />)
    expect(layout()).toBe('horizontal')
  })

  it('stacks when it does not fit and goes back once there is room', () => {
    const width = { client: 600, scroll: 700 }
    const resize = sized(width)
    render(<RequestFlow {...base} />)
    expect(layout()).toBe('vertical')
    expect(screen.getByTestId('request-flow').className).not.toMatch(/overflow-x-auto|overflow-x-scroll/)
    width.client = 800
    resize()
    expect(layout()).toBe('horizontal')
  })

  it('stacks again when the box shrinks', () => {
    const width = { client: 900, scroll: 700 }
    const resize = sized(width)
    render(<RequestFlow {...base} />)
    expect(layout()).toBe('horizontal')
    width.client = 600
    resize()
    expect(layout()).toBe('vertical')
  })
})

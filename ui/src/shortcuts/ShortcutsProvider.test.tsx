import { Button, Dialog, LinkButton, Radio, Select } from '@cloudflare/kumo'
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { useState, type ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AppLayout } from '../layout/AppLayout'
import { mailRoutes } from '../test/mailFixtures'
import { webRoutes } from '../test/webFixtures'
import { listPages } from '../test/listPages'
import { dnsRoutes, renderApp, signedIn, zone } from '../test/renderApp'
import { popupOpen, SEARCH_FIELD_MEDIA, useShortcutHandler } from './ShortcutsProvider'
import { shortcutTarget } from './targets'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const seen: string[] = []

/** A page inside the app shell that records the keys the engine hands to row handlers. */
function KeyProbe({ children }: { children?: ReactNode }) {
  useShortcutHandler((e) => {
    seen.push(e.key)
    return e.key === 'x'
  })
  return (
    <div>
      <input aria-label="Name" />
      {children}
    </div>
  )
}

function DialogProbe() {
  const [open, setOpen] = useState(false)
  return (
    <KeyProbe>
      <Button onClick={() => setOpen(true)}>Open dialog</Button>
      <Dialog.Root open={open} onOpenChange={setOpen}>
        <Dialog className="px-6 py-5">
          <Dialog.Title>Probe dialog</Dialog.Title>
        </Dialog>
      </Dialog.Root>
    </KeyProbe>
  )
}

function inShell(path: string, element: ReactNode) {
  return [{ path, element: <AppLayout />, children: [{ index: true, element }] }]
}

const routes = { 'GET /bootstrap': () => ({ body: signedIn({}) }) }

async function probe(element: ReactNode = <KeyProbe />) {
  seen.length = 0
  const app = renderApp('/probe', routes, inShell('/probe', element))
  await screen.findByRole('button', { name: 'admin' })
  return app
}

describe('shortcut engine', () => {
  it('letters do nothing while typing', async () => {
    await probe()
    const input = screen.getByRole('textbox', { name: 'Name' })
    input.focus()
    expect(fireEvent.keyDown(input, { key: 'x' })).toBe(true)
    expect(seen).toEqual([])
    fireEvent.keyDown(document.body, { key: 'x' })
    expect(seen).toEqual(['x'])
  })

  it('letters do nothing while a dialog or menu is open', async () => {
    await probe(<DialogProbe />)
    fireEvent.click(screen.getByRole('button', { name: 'Open dialog' }))
    await screen.findByRole('dialog')
    fireEvent.keyDown(document.body, { key: 'x' })
    expect(seen).toEqual([])
    fireEvent.keyDown(document.body, { key: 'Escape' })
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())

    fireEvent.click(screen.getByRole('button', { name: 'admin' }))
    await screen.findByRole('menu')
    fireEvent.keyDown(document.body, { key: 'x' })
    expect(seen).toEqual([])
  })

  it('unhandled keys are not prevented', async () => {
    await probe()
    expect(fireEvent.keyDown(document.body, { key: 'z' })).toBe(true)
    expect(fireEvent.keyDown(document.body, { key: 'ArrowDown' })).toBe(true)
    expect(fireEvent.keyDown(document.body, { key: 'x' })).toBe(false)
    expect(seen).toEqual(['z', 'ArrowDown', 'x'])
  })

  it('escape blurs the focused field when no popup is open', async () => {
    await probe()
    const input = screen.getByRole('textbox', { name: 'Name' })
    input.focus()
    expect(document.activeElement).toBe(input)
    fireEvent.keyDown(input, { key: 'Escape' })
    expect(document.activeElement).not.toBe(input)
  })

  it('ignores the key that ends an IME composition', async () => {
    await probe()
    const input = screen.getByRole('textbox', { name: 'Name' })
    input.focus()
    // Safari commits a composition with isComposing false and keyCode 229.
    expect(fireEvent.keyDown(input, { key: 'Escape', keyCode: 229 })).toBe(true)
    expect(document.activeElement).toBe(input)
    fireEvent.keyDown(document.body, { key: 'x', keyCode: 229 })
    expect(seen).toEqual([])
  })

  it('keeps ctrl+k for the command palette', async () => {
    await probe()
    fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
    expect(await screen.findByPlaceholderText(/Search pages, domains/)).toBeTruthy()
  })
})

function AddProbe() {
  return (
    <LinkButton href="/dns" {...shortcutTarget('add')}>
      Add probe
    </LinkButton>
  )
}

function SaveProbe({ onSave }: { onSave: (name: string) => void }) {
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        onSave(new FormData(e.currentTarget).get('name') as string)
      }}
    >
      <input aria-label="Name" name="name" defaultValue="web1" />
      <Button type="submit" {...shortcutTarget('save')}>
        Save
      </Button>
    </form>
  )
}

/** A form whose Radio and Select trigger take Enter for themselves. */
function ControlsSaveProbe({ onSave }: { onSave: () => void }) {
  const [pick, setPick] = useState('a')
  const [sel, setSel] = useState('a')
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        onSave()
      }}
    >
      <Radio.Group legend="Pick" value={pick} onValueChange={(v) => setPick(String(v))}>
        <Radio.Item label="Pick A" value="a" />
        <Radio.Item label="Pick B" value="b" />
      </Radio.Group>
      <Select label="Choice" value={sel} onValueChange={(v) => setSel(String(v))} items={{ a: 'Alpha', b: 'Beta' }}>
        <Select.Option value="a">Alpha</Select.Option>
        <Select.Option value="b">Beta</Select.Option>
      </Select>
      <Button type="submit" {...shortcutTarget('save')}>
        Save
      </Button>
    </form>
  )
}

/** The search field shows from md up (48rem); the test decides whether it is wide. */
function wide(on: boolean) {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: on && query === SEARCH_FIELD_MEDIA,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  }))
}

describe('page shortcuts', () => {
  it("a follows the page's add button", async () => {
    const app = renderApp('/probe', dnsRoutes(), inShell('/probe', <AddProbe />))
    await screen.findByRole('link', { name: 'Add probe' })
    expect(fireEvent.keyDown(document.body, { key: 'a' })).toBe(false)
    await waitFor(() => expect(app.location()).toBe('/dns'))
  })

  it('a does nothing without one', async () => {
    const app = await probe()
    expect(fireEvent.keyDown(document.body, { key: 'a' })).toBe(true)
    expect(app.location()).toBe('/probe')
  })

  it('f focuses the top-bar search', async () => {
    wide(true)
    await probe()
    expect(fireEvent.keyDown(document.body, { key: 'f' })).toBe(false)
    expect(document.activeElement).toBe(screen.getByRole('searchbox', { name: 'Search' }))
    expect(screen.queryByPlaceholderText(/Search pages, domains/)).toBeNull()
  })

  it('f opens the palette when the search field is hidden', async () => {
    wide(false)
    await probe()
    fireEvent.keyDown(document.body, { key: 'f' })
    expect(await screen.findByPlaceholderText(/Search pages, domains/)).toBeTruthy()
  })

  it('1 to 6 go to web dns mail databases cron backups', async () => {
    const app = await probe()
    const paths = ['/web', '/dns', '/mail', '/db', '/cron', '/backup']
    for (const [i, path] of paths.entries()) {
      expect(fireEvent.keyDown(document.body, { key: String(i + 1) })).toBe(false)
      await waitFor(() => expect(app.location()).toBe(path))
    }
    expect(fireEvent.keyDown(document.body, { key: '7' })).toBe(true)
    expect(app.location()).toBe('/backup')
  })

  it('a number for a section that is off does nothing', async () => {
    seen.length = 0
    const user = signedIn({}).user
    const app = renderApp(
      '/probe',
      { 'GET /bootstrap': () => ({ body: signedIn({ features: { ...user.features, web: false, cron: false } }) }) },
      inShell('/probe', <KeyProbe />),
    )
    await screen.findByRole('button', { name: 'admin' })
    expect(fireEvent.keyDown(document.body, { key: '1' })).toBe(true)
    fireEvent.keyDown(document.body, { key: '5' })
    expect(app.location()).toBe('/probe')
    fireEvent.keyDown(document.body, { key: '2' })
    await waitFor(() => expect(app.location()).toBe('/dns'))
  })

  it('ctrl+backspace follows the back link', async () => {
    const app = renderApp('/mail/example.com', mailRoutes())
    await waitFor(() => expect(document.querySelector('[data-shortcut="back"]')?.textContent).toBe('Mail'))
    expect(fireEvent.keyDown(document.body, { key: 'Backspace' })).toBe(true)
    expect(app.location()).toBe('/mail/example.com')
    expect(fireEvent.keyDown(document.body, { key: 'Backspace', ctrlKey: true })).toBe(false)
    await waitFor(() => expect(app.location()).toBe('/mail'))
  })

  it('ctrl+enter submits the form through its save button', async () => {
    const saved: string[] = []
    renderApp('/probe', routes, inShell('/probe', <SaveProbe onSave={(n) => saved.push(n)} />))
    const input = await screen.findByRole('textbox', { name: 'Name' })
    input.focus()
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(saved).toEqual([])
    expect(fireEvent.keyDown(input, { key: 'Enter', ctrlKey: true })).toBe(false)
    expect(saved).toEqual(['web1'])
  })

  it.each([
    ['a Radio', () => screen.getByRole('radio', { name: 'Pick A' })],
    ['a Select trigger', () => screen.getByRole('combobox', { name: 'Choice' })],
  ])('ctrl+enter saves with focus on %s', async (_name, control) => {
    let saved = 0
    renderApp('/probe', routes, inShell('/probe', <ControlsSaveProbe onSave={() => saved++} />))
    await screen.findByRole('radio', { name: 'Pick A' })
    const el = control()
    el.focus()
    expect(document.activeElement).toBe(el)
    fireEvent.keyDown(el, { key: 'Enter', ctrlKey: true })
    expect(saved).toBe(1)
    expect(screen.queryByRole('listbox')).toBeNull()
  })

  it('ctrl+enter does not save while a dialog is open', async () => {
    const saved: string[] = []
    renderApp(
      '/probe',
      routes,
      inShell(
        '/probe',
        <>
          <SaveProbe onSave={(n) => saved.push(n)} />
          <DialogProbe />
        </>,
      ),
    )
    fireEvent.click(await screen.findByRole('button', { name: 'Open dialog' }))
    await screen.findByRole('dialog')
    fireEvent.keyDown(document.body, { key: 'Enter', ctrlKey: true })
    expect(saved).toEqual([])
  })

  it('ctrl+enter saves a changed form page without asking to leave', async () => {
    const app = renderApp('/dns/example.com/edit', {
      ...dnsRoutes(),
      'GET /dns/example.com': () => ({ body: zone('example.com', { SOA: 'ns9.example.net', TTL: '300' }) }),
      'PUT /dns/example.com': () => ({ body: { status: 'ok' } }),
    })
    const soa = (await screen.findByLabelText('SOA', undefined, { timeout: 10000 })) as HTMLInputElement
    fireEvent.change(soa, { target: { value: 'ns1.example.net' } })
    soa.focus()
    expect(fireEvent.keyDown(soa, { key: 'Enter', ctrlKey: true })).toBe(false)
    await waitFor(() => expect(app.location()).toBe('/dns'), { timeout: 10000 })
    expect(app.calls.find((c) => c.key === 'PUT /dns/example.com')?.body).toMatchObject({ soa: 'ns1.example.net' })
    expect(screen.queryByRole('dialog', { name: 'Unsaved changes' })).toBeNull()
  }, 20000)

  it('ctrl+shift+a selects every row on a list', async () => {
    renderApp('/dns', dnsRoutes())
    await screen.findByRole('link', { name: 'example.com' })
    const boxes = () => screen.getAllByRole('checkbox', { name: /^Select example/ })
    expect(boxes().map((b) => b.getAttribute('aria-checked'))).toEqual(['false', 'false'])
    expect(fireEvent.keyDown(document.body, { key: 'A', shiftKey: true, ctrlKey: true })).toBe(false)
    await waitFor(() => expect(boxes().map((b) => b.getAttribute('aria-checked'))).toEqual(['true', 'true']))
    fireEvent.keyDown(document.body, { key: 'A', shiftKey: true, ctrlKey: true })
    await waitFor(() => expect(boxes().map((b) => b.getAttribute('aria-checked'))).toEqual(['false', 'false']))
  })
})

describe('add buttons', () => {
  it.each(listPages)('a opens the add form on every list: $name', async ({ path, routes, add }) => {
    const app = renderApp(path, routes())
    await waitFor(() => expect(document.querySelector('[data-shortcut="add"]')).toBeTruthy(), { timeout: 10000 })
    expect(fireEvent.keyDown(document.body, { key: 'a' })).toBe(false)
    if ('to' in add) await waitFor(() => expect(app.location()).toBe(add.to))
    else await waitFor(() => expect(app.calls.map((c) => c.key)).toContain(add.call))
  }, 20000)
})

describe('toasts', () => {
  // Kumo toasts are role="dialog" with aria-modal="false"; they must not count as an open popup.
  it('down and delete still work while a toast is showing', async () => {
    const app = renderApp('/web', webRoutes({}, { 'POST /web/example.com/suspend': () => ({ body: { status: 'ok' } }) }))
    await waitFor(() => expect(document.querySelectorAll('tbody tr').length).toBe(2), { timeout: 10000 })
    const rows = () => document.querySelectorAll('tbody tr')
    fireEvent.keyDown(document.body, { key: 'ArrowDown' })
    await waitFor(() => expect(rows()[0].hasAttribute('data-row-focused')).toBe(true))
    fireEvent.keyDown(document.body, { key: 's' })
    const ask = await screen.findByRole('dialog', { name: 'Suspend web domain' })
    fireEvent.click(within(ask).getByRole('button', { name: 'Suspend' }))
    expect(await screen.findByText('example.com suspended')).toBeTruthy()
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Suspend web domain' })).toBeNull())
    expect(app.calls.map((c) => c.key)).toContain('POST /web/example.com/suspend')

    // The toast is still on screen.
    expect(screen.getByText('example.com suspended')).toBeTruthy()
    expect(fireEvent.keyDown(document.body, { key: 'ArrowDown' })).toBe(false)
    await waitFor(() => expect(rows()[1].hasAttribute('data-row-focused')).toBe(true))
    fireEvent.keyDown(document.body, { key: 'Delete' })
    expect(await screen.findByRole('dialog', { name: 'Delete example.net' })).toBeTruthy()
  }, 20000)
})

/**
 * A Select list as Base UI leaves it in a real browser. After a Select closes (and on
 * some pages before it ever opens) the list stays mounted: the popup gets data-closed
 * and its positioner gets the hidden attribute. Open, they carry data-open instead.
 */
function mountList(state: 'open' | 'closed') {
  const portal = document.createElement('div')
  portal.setAttribute('data-base-ui-portal', '')
  portal.setAttribute('data-probe-list', '')
  const closed = state === 'closed'
  portal.innerHTML = `
    <div role="presentation" ${closed ? 'data-closed hidden' : 'data-open'}>
      <div role="presentation" ${closed ? 'data-closed' : 'data-open'}>
        <div role="listbox" id="probe-list"><div role="option">Alpha</div></div>
      </div>
    </div>`
  document.body.append(portal)
  return portal
}

describe('select lists', () => {
  afterEach(() => document.querySelectorAll('[data-probe-list]').forEach((el) => el.remove()))

  it('a closed list that stays mounted is not an open popup', () => {
    mountList('closed')
    expect(document.querySelector('[role="listbox"]')).toBeTruthy()
    expect(popupOpen()).toBe(false)
  })

  it('an open list is an open popup', () => {
    mountList('open')
    expect(popupOpen()).toBe(true)
  })

  it('a list hidden by layout alone is not an open popup', () => {
    const portal = mountList('open')
    ;(portal.firstElementChild as HTMLElement).style.display = 'none'
    expect(popupOpen()).toBe(false)
  })

  it('without checkVisibility a list with no boxes is not an open popup', () => {
    mountList('open')
    const own = Element.prototype.checkVisibility
    // Older browsers: fall back to the element's layout boxes.
    Object.defineProperty(Element.prototype, 'checkVisibility', { value: undefined, configurable: true, writable: true })
    try {
      expect(popupOpen()).toBe(false)
      const list = document.querySelector('[role="listbox"]')!
      vi.spyOn(list, 'getClientRects').mockReturnValue([new DOMRect(0, 0, 100, 40)] as unknown as DOMRectList)
      expect(popupOpen()).toBe(true)
    } finally {
      Object.defineProperty(Element.prototype, 'checkVisibility', { value: own, configurable: true, writable: true })
    }
  })

  it('keys and ctrl+enter work while a closed list stays mounted', async () => {
    const saved: string[] = []
    await probe(
      <KeyProbe>
        <SaveProbe onSave={(n) => saved.push(n)} />
      </KeyProbe>,
    )
    mountList('closed')
    expect(fireEvent.keyDown(document.body, { key: 'x' })).toBe(false)
    expect(seen).toEqual(['x'])
    fireEvent.keyDown(document.body, { key: 'Enter', ctrlKey: true })
    expect(saved).toEqual(['web1'])
  })

  it('ctrl+enter that an open list takes and closes on does not also save', async () => {
    const saved: string[] = []
    await probe(<SaveProbe onSave={(n) => saved.push(n)} />)
    const portal = mountList('open')
    const list = portal.querySelector<HTMLElement>('[role="listbox"]')!
    // As Base UI does in Chromium: Enter picks the option, prevents the key and closes
    // the list, all before the key reaches the window.
    list.addEventListener('keydown', (e) => {
      e.preventDefault()
      portal.querySelectorAll('[data-open]').forEach((el) => {
        el.removeAttribute('data-open')
        el.setAttribute('data-closed', '')
      })
    })
    fireEvent.keyDown(list, { key: 'Enter', ctrlKey: true })
    expect(list.closest('[data-closed]')).toBeTruthy()
    expect(saved).toEqual([])
  })

  it('keys and ctrl+enter do nothing while a list is open', async () => {
    const saved: string[] = []
    await probe(
      <KeyProbe>
        <SaveProbe onSave={(n) => saved.push(n)} />
      </KeyProbe>,
    )
    mountList('open')
    fireEvent.keyDown(document.body, { key: 'x' })
    fireEvent.keyDown(document.body, { key: 'Enter', ctrlKey: true })
    expect(seen).toEqual([])
    expect(saved).toEqual([])
  })
})

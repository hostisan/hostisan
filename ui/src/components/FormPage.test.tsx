import { Button, Checkbox, Dialog, Input, Select, Switch } from '@cloudflare/kumo'
import { useMutation } from '@tanstack/react-query'
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { useState, type ReactNode } from 'react'
import { Link, Navigate, useNavigate } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../api/client'
import { AppLayout } from '../layout/AppLayout'
import { useSession } from '../session/SessionProvider'
import { renderApp, signedIn, type Route } from '../test/renderApp'
import { ComboboxField, DateField, NameServersField, SelectField } from './fields'
import { FormPage } from './FormPage'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

/** As App's RequireSession: the shell renders only for a signed-in session. */
function SignedIn() {
  const { bootstrap } = useSession()
  if (!bootstrap) return null
  if (!bootstrap.authenticated) return <Navigate to="/login" replace />
  return <AppLayout />
}

const back = { href: '/dns', label: 'DNS' }

function Shell({ children, dirty }: { children: ReactNode; dirty?: boolean }) {
  return (
    <FormPage title="Probe" back={back} dirty={dirty}>
      {children}
      <Link to="/dns">Elsewhere</Link>
    </FormPage>
  )
}

function TextPage() {
  return (
    <Shell>
      <Input label="Name" name="name" />
    </Shell>
  )
}

function KumoControlsPage() {
  const [sw, setSw] = useState(false)
  const [cb, setCb] = useState(false)
  const [sel, setSel] = useState('a')
  return (
    <Shell>
      <Switch label="Direct switch" checked={sw} onCheckedChange={setSw} />
      <Checkbox label="Direct checkbox" checked={cb} onCheckedChange={(v) => setCb(v === true)} />
      <Select
        label="Direct select"
        value={sel}
        onValueChange={(v) => setSel(v as string)}
        items={{ a: 'Alpha', b: 'Beta' }}
      >
        <Select.Option value="a">Alpha</Select.Option>
        <Select.Option value="b">Beta</Select.Option>
      </Select>
    </Shell>
  )
}

const tz = [
  { value: 'UTC', label: 'UTC' },
  { value: 'Europe/Paris', label: 'Europe Paris' },
]

function FieldsPage() {
  const [sel, setSel] = useState('a')
  const [date, setDate] = useState('2026-01-15')
  return (
    <Shell>
      <SelectField
        label="Shared select"
        value={sel}
        onChange={setSel}
        options={[
          { value: 'a', label: 'Alpha' },
          { value: 'b', label: 'Beta' },
        ]}
      />
      <ComboboxField label="Time zone" options={tz} defaultValue="UTC" />
      <DateField label="Expires" value={date} onChange={setDate} />
      <NameServersField defaultValue={['ns1.example.net', 'ns2.example.net']} />
    </Shell>
  )
}

function SavePage() {
  const navigate = useNavigate()
  const save = useMutation({ mutationFn: (name: string) => api.post('/thing', { name }) })
  const [error, setError] = useState('')
  return (
    <FormPage
      title="Probe"
      back={back}
      onSubmit={async (e) => {
        const name = String(new FormData(e.currentTarget as HTMLFormElement).get('name'))
        try {
          await save.mutateAsync(name)
          navigate('/dns')
        } catch {
          setError('Save failed')
        }
      }}
    >
      <Input label="Name" name="name" />
      {error && <p>{error}</p>}
      <Link to="/dns">Elsewhere</Link>
    </FormPage>
  )
}

function StayPage() {
  const save = useMutation({ mutationFn: () => api.post('/thing', {}) })
  const [saved, setSaved] = useState(false)
  return (
    <FormPage
      title="Probe"
      back={back}
      onSubmit={async () => {
        await save.mutateAsync()
        setSaved(true)
      }}
    >
      <Input label="Name" name="name" />
      {saved && <p>Saved here</p>}
      <Link to="/dns">Elsewhere</Link>
    </FormPage>
  )
}

function DirtyPropPage() {
  const [forced, setForced] = useState<boolean | undefined>(false)
  return (
    <Shell dirty={forced}>
      <Input label="Name" name="name" />
      <button type="button" onClick={() => setForced(true)}>
        Force dirty
      </button>
    </Shell>
  )
}

/** A dialog inside the form page with its own fields, as the 2FA and delete dialogs. */
function DialogPage() {
  const [open, setOpen] = useState(false)
  const [sel, setSel] = useState('a')
  return (
    <Shell>
      <Input label="Name" name="name" />
      <Button onClick={() => setOpen(true)}>Open dialog</Button>
      <Dialog.Root open={open} onOpenChange={setOpen}>
        <Dialog className="grid gap-4 px-6 py-5">
          <Dialog.Title>Probe dialog</Dialog.Title>
          <Input label="Dialog note" />
          <Select
            label="Dialog select"
            value={sel}
            onValueChange={(v) => setSel(v as string)}
            items={{ a: 'Alpha', b: 'Beta' }}
          >
            <Select.Option value="a">Alpha</Select.Option>
            <Select.Option value="b">Beta</Select.Option>
          </Select>
          <Button onClick={() => setOpen(false)}>Done</Button>
        </Dialog>
      </Dialog.Root>
    </Shell>
  )
}

/** Saves and stays; a separate button runs a mutation that has nothing to do with the form. */
function BackgroundPage() {
  const save = useMutation({ mutationFn: () => api.post('/thing', {}) })
  const other = useMutation({ mutationFn: () => api.post('/other', {}) })
  const [saved, setSaved] = useState(false)
  return (
    <FormPage
      title="Probe"
      back={back}
      onSubmit={async () => {
        await save.mutateAsync()
        setSaved(true)
      }}
    >
      <Input label="Name" name="name" />
      {saved && <p>Saved here</p>}
      <button type="button" onClick={() => other.mutate()}>
        Mark read
      </button>
      {other.isError && <p>Other failed</p>}
      <Link to="/dns">Elsewhere</Link>
    </FormPage>
  )
}

/** A save stopped by a client-side check: it calls guard.rearm(). */
function CheckPage() {
  const [error, setError] = useState('')
  return (
    <FormPage
      title="Probe"
      back={back}
      onSubmit={(e, guard) => {
        if (!new FormData(e.currentTarget).get('name')) {
          guard.rearm()
          setError('Name is required')
        }
      }}
    >
      <Input label="Name" name="name" />
      <Input label="Note" name="note" />
      {error && <p>{error}</p>}
      <Link to="/dns">Elsewhere</Link>
    </FormPage>
  )
}

const pages = [
  {
    element: <SignedIn />,
    children: [
      { path: '/t/text', element: <TextPage /> },
      { path: '/t/kumo', element: <KumoControlsPage /> },
      { path: '/t/fields', element: <FieldsPage /> },
      { path: '/t/save', element: <SavePage /> },
      { path: '/t/stay', element: <StayPage /> },
      { path: '/t/dirty', element: <DirtyPropPage /> },
      { path: '/t/dialog', element: <DialogPage /> },
      { path: '/t/background', element: <BackgroundPage /> },
      { path: '/t/check', element: <CheckPage /> },
    ],
  },
]

function routes(over: Record<string, Route> = {}): Record<string, Route> {
  return {
    'GET /bootstrap': () => ({ body: signedIn({}) }),
    'GET /dns': () => ({
      body: { zones: [], usage: { zones: '0', zonesLimit: '5', suspended: '0', records: '0', recordsLimit: '250' } },
    }),
    'POST /thing': () => ({ body: { status: 'ok' } }),
    ...over,
  }
}

const dialog = () => screen.queryByRole('dialog', { name: 'Unsaved changes' })

/** Clicks the "Elsewhere" link and reports whether the page asked. */
async function leaveAsks(app: ReturnType<typeof renderApp>, from: string): Promise<boolean> {
  fireEvent.click(screen.getByRole('link', { name: 'Elsewhere' }))
  await waitFor(() => expect(dialog() !== null || app.location() !== from).toBe(true))
  return dialog() !== null
}

async function type(value = 'changed') {
  fireEvent.change(await screen.findByRole('textbox', { name: 'Name' }), { target: { value } })
}

async function stay() {
  fireEvent.click(await screen.findByRole('button', { name: 'Stay on page' }))
  await waitFor(() => expect(dialog()).toBeNull())
}

describe('FormPage unsaved changes', () => {
  it('typing marks the form changed', async () => {
    const app = renderApp('/t/text', routes(), pages)
    await screen.findByRole('textbox', { name: 'Name' })
    await type()
    expect(await leaveAsks(app, '/t/text')).toBe(true)
  })

  it('an untouched form leaves without asking', async () => {
    const app = renderApp('/t/text', routes(), pages)
    await screen.findByRole('textbox', { name: 'Name' })
    expect(await leaveAsks(app, '/t/text')).toBe(false)
  })

  // One case per control, so a slow machine doesn't time out a single long test.
  const controls: [string, string, () => Promise<void>][] = [
    [
      'a direct Kumo switch',
      '/t/kumo',
      async () => {
        fireEvent.click(await screen.findByRole('switch', { name: 'Direct switch' }))
      },
    ],
    [
      'a direct Kumo checkbox',
      '/t/kumo',
      async () => {
        fireEvent.click(await screen.findByRole('checkbox', { name: 'Direct checkbox' }))
      },
    ],
    [
      'a direct Kumo select',
      '/t/kumo',
      async () => {
        fireEvent.click(await screen.findByRole('combobox', { name: 'Direct select' }))
        fireEvent.click(await screen.findByRole('option', { name: 'Beta' }))
      },
    ],
    [
      'SelectField',
      '/t/fields',
      async () => {
        fireEvent.click(await screen.findByRole('combobox', { name: 'Shared select' }))
        fireEvent.click(await screen.findByRole('option', { name: 'Beta' }))
      },
    ],
    [
      'ComboboxField',
      '/t/fields',
      async () => {
        const combo = await screen.findByRole('combobox', { name: 'Time zone' })
        fireEvent.click(combo)
        fireEvent.keyDown(combo, { key: 'ArrowDown' })
        fireEvent.click(await screen.findByRole('option', { name: 'Europe Paris' }))
      },
    ],
    [
      'DateField',
      '/t/fields',
      async () => {
        fireEvent.click(await screen.findByRole('button', { name: '2026-01-15' }))
        fireEvent.click(await screen.findByRole('button', { name: /January 20/ }))
      },
    ],
    [
      'NameServersField',
      '/t/fields',
      async () => {
        fireEvent.click(await screen.findByRole('button', { name: /Remove ns1\.example\.net/i }))
      },
    ],
  ]

  it.each(controls)('a Kumo select, switch, checkbox and combobox mark it changed: %s', async (_name, path, act) => {
    const app = renderApp(path, routes(), pages)
    await act()
    expect(await leaveAsks(app, path)).toBe(true)
  })

  it('picking a Kumo select option with the keyboard marks it changed', async () => {
    const app = renderApp('/t/kumo', routes(), pages)
    fireEvent.click(await screen.findByRole('combobox', { name: 'Direct select' }))
    fireEvent.keyDown(await screen.findByRole('option', { name: 'Beta' }), { key: 'Enter' })
    expect(await leaveAsks(app, '/t/kumo')).toBe(true)
  })

  it('a successful save that navigates does not ask', async () => {
    const app = renderApp('/t/save', routes(), pages)
    await type()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(app.location()).toBe('/dns'))
    expect(dialog()).toBeNull()
  })

  it('the default save toasts and goes back without asking', async () => {
    const app = renderApp('/t/text', routes(), pages)
    await type()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(app.location()).toBe('/dns'))
    expect(dialog()).toBeNull()
    expect(await screen.findByText('Changes have been saved.')).toBeTruthy()
  })

  it('a failed save keeps asking', async () => {
    const app = renderApp(
      '/t/save',
      routes({ 'POST /thing': () => ({ status: 500, body: { error: { code: 'server_error', message: 'No' } } }) }),
      pages,
    )
    await type()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Save failed')).toBeTruthy()
    expect(await leaveAsks(app, '/t/save')).toBe(true)
  })

  it('a later change after a save asks again', async () => {
    const app = renderApp('/t/stay', routes(), pages)
    await type('first')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Saved here')).toBeTruthy()
    expect(await leaveAsks(app, '/t/stay')).toBe(false)
    cleanup()

    const again = renderApp('/t/stay', routes(), pages)
    await type('first')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Saved here')).toBeTruthy()
    await type('second')
    expect(await leaveAsks(again, '/t/stay')).toBe(true)
  })

  it('the dirty prop overrides detection', async () => {
    const app = renderApp('/t/dirty', routes(), pages)
    await type()
    // dirty={false}: typing alone does not arm it.
    expect(await leaveAsks(app, '/t/dirty')).toBe(false)
    cleanup()

    const forced = renderApp('/t/dirty', routes(), pages)
    fireEvent.click(await screen.findByRole('button', { name: 'Force dirty' }))
    expect(await leaveAsks(forced, '/t/dirty')).toBe(true)
    await stay()
    expect(forced.location()).toBe('/t/dirty')
  })

  it('the default save with the dirty prop set goes back without asking', async () => {
    const app = renderApp('/t/dirty', routes(), pages)
    fireEvent.click(await screen.findByRole('button', { name: 'Force dirty' }))
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(app.location()).toBe('/dns'))
    expect(dialog()).toBeNull()
  })

  it('typing or picking in a dialog inside the page does not mark it changed', async () => {
    const app = renderApp('/t/dialog', routes(), pages)
    fireEvent.click(await screen.findByRole('button', { name: 'Open dialog' }))
    fireEvent.change(await screen.findByRole('textbox', { name: 'Dialog note' }), { target: { value: 'x' } })
    fireEvent.click(screen.getByRole('combobox', { name: 'Dialog select' }))
    fireEvent.click(await screen.findByRole('option', { name: 'Beta' }))
    fireEvent.click(screen.getByRole('button', { name: 'Done' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Probe dialog' })).toBeNull())
    expect(await leaveAsks(app, '/t/dialog')).toBe(false)
  })

  it("an unrelated mutation's failure does not re-arm a saved form", async () => {
    const app = renderApp(
      '/t/background',
      routes({ 'POST /other': () => ({ status: 500, body: { error: { code: 'server_error', message: 'No' } } }) }),
      pages,
    )
    await type()
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Saved here')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Mark read' }))
    expect(await screen.findByText('Other failed')).toBeTruthy()
    expect(await leaveAsks(app, '/t/background')).toBe(false)
  })

  it('a save stopped by a client-side check keeps asking', async () => {
    const app = renderApp('/t/check', routes(), pages)
    fireEvent.change(await screen.findByRole('textbox', { name: 'Note' }), { target: { value: 'x' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(await screen.findByText('Name is required')).toBeTruthy()
    expect(await leaveAsks(app, '/t/check')).toBe(true)
  })

  it('cancel asks when changed', async () => {
    const app = renderApp('/t/text', routes(), pages)
    await type()
    fireEvent.click(screen.getByRole('link', { name: 'Cancel' }))
    expect(await screen.findByRole('dialog', { name: 'Unsaved changes' })).toBeTruthy()
    expect(app.location()).toBe('/t/text')
  })

  it('the save button carries data-shortcut save', async () => {
    renderApp('/t/text', routes(), pages)
    const save = await screen.findByRole('button', { name: 'Save' })
    expect(save.getAttribute('data-shortcut')).toBe('save')
    expect(document.querySelectorAll('[data-shortcut="save"]')).toHaveLength(1)
  })

  it('Save, Cancel and Changes saved are translated', async () => {
    const app = renderApp(
      '/t/text',
      routes({
        'GET /bootstrap': () => ({ body: { ...signedIn({}), language: 'fr' } }),
        'GET /i18n/fr?v=1.0.0': () => ({
          body: {
            Save: 'Enregistrer',
            Cancel: 'Annuler',
            'Changes have been saved.': 'Les modifications ont été enregistrées.',
          },
        }),
      }),
      pages,
    )
    const bar = (await screen.findByRole('button', { name: 'Enregistrer' })).parentElement as HTMLElement
    expect(within(bar).getByRole('link', { name: 'Annuler' })).toBeTruthy()
    fireEvent.click(within(bar).getByRole('button', { name: 'Enregistrer' }))
    await waitFor(() => expect(app.location()).toBe('/dns'))
    expect(await screen.findByText('Les modifications ont été enregistrées.')).toBeTruthy()
  })
})

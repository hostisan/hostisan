// Every FormPage route asks before leaving once any kind of control on it has changed.
//
// Each case opens a form page over its fixtures, changes the first enabled control of
// one kind inside the form, and clicks the sidebar's "Web" link.
import { cleanup, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  accountOptions,
  backupRoutes,
  cronRoutes,
  keysRoutes,
  profileRoutes,
  usersRoutes,
} from '../test/accountFixtures'
import { mailOptions, mailRoutes } from '../test/mailFixtures'
import { database, dbRoutes, dnsRoutes, renderApp, zone, type Route } from '../test/renderApp'
import { serverRoutes } from '../test/serverFixtures'
import { webAppForm, webRoutes } from '../test/webFixtures'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

type Kind = 'text' | 'checkbox' | 'switch' | 'radio' | 'select' | 'combobox' | 'tag'

type Page = { name: string; path: string; routes: () => Record<string, Route> }

const full = {
  web: true,
  dns: true,
  db: true,
  mail: true,
  cron: true,
  backup: true,
  incremental: true,
  logs: true,
  accessKeys: true,
  server: true,
  firewall: true,
  firewallBans: true,
}

const accountRoutes = () => ({ ...usersRoutes(), ...profileRoutes() })

const pages: Page[] = [
  { name: 'AccessKeysPage', path: '/account/access-keys/new', routes: () => keysRoutes() },
  { name: 'AccountPage', path: '/account', routes: accountRoutes },
  { name: 'SshKeysPage', path: '/account/ssh-keys/new', routes: () => keysRoutes() },
  { name: 'ExclusionsPages', path: '/backup/exclusions/edit', routes: () => backupRoutes() },
  { name: 'CronFormPage', path: '/cron/new', routes: () => cronRoutes() },
  { name: 'DatabaseFormPage (add)', path: '/db/new', routes: () => dbRoutes() },
  {
    name: 'DatabaseFormPage (edit)',
    path: '/db/admin_wordpress',
    routes: () => ({
      ...dbRoutes(),
      'GET /db/admin_wordpress': () => ({ body: database('admin_wordpress', { DBUSER: 'admin_wpuser' }) }),
    }),
  },
  { name: 'DnsRecordFormPage', path: '/dns/example.com/records/new', routes: () => dnsRoutes() },
  { name: 'DnsZoneFormPage (add)', path: '/dns/new', routes: () => dnsRoutes() },
  {
    name: 'DnsZoneFormPage (edit)',
    path: '/dns/example.com/edit',
    routes: () => ({
      ...dnsRoutes(),
      'GET /dns/example.com': () => ({ body: zone('example.com', { SOA: 'ns9.example.net', TTL: '300' }) }),
    }),
  },
  { name: 'MailAccountFormPage', path: '/mail/example.com/accounts/new', routes: () => mailRoutes({ features: full }) },
  {
    name: 'MailDomainFormPage (add)',
    path: '/mail/new',
    routes: () => mailRoutes({ features: full }, { 'GET /mail/options': () => ({ body: mailOptions() }) }),
  },
  {
    name: 'MailDomainFormPage (edit)',
    path: '/mail/example.com/edit',
    routes: () => mailRoutes({ features: full }, { 'GET /mail/options': () => ({ body: mailOptions() }) }),
  },
  { name: 'FirewallBanFormPage', path: '/server/firewall/banlist/new', routes: () => serverRoutes() },
  { name: 'FirewallIpsetFormPage', path: '/server/firewall/ipsets/new', routes: () => serverRoutes() },
  { name: 'FirewallRuleFormPage', path: '/server/firewall/new', routes: () => serverRoutes() },
  { name: 'IpFormPage', path: '/server/ip/new', routes: () => serverRoutes() },
  { name: 'ServerConfigPage', path: '/server/config', routes: () => serverRoutes() },
  { name: 'ServiceEditPage', path: '/server/services/nginx', routes: () => serverRoutes() },
  { name: 'WhiteLabelPage', path: '/server/white-label', routes: () => serverRoutes() },
  { name: 'PackageFormPage', path: '/packages/new', routes: () => usersRoutes({ features: full }) },
  { name: 'UserEditPage', path: '/users/bob', routes: accountRoutes },
  {
    name: 'UserForm',
    path: '/users/new',
    routes: () => ({ ...accountRoutes(), 'GET /account/options': () => ({ body: accountOptions() }) }),
  },
  { name: 'AddWebDomainPage', path: '/web/new', routes: () => webRoutes({ name: 'bob', role: 'user' }) },
  { name: 'EditWebDomainPage', path: '/web/example.com', routes: () => webRoutes() },
  { name: 'GenerateSslPage', path: '/web/example.com/ssl/generate', routes: () => webRoutes() },
  {
    name: 'SetupWebAppPage',
    path: '/web/example.com/apps/wordpress',
    routes: () =>
      webRoutes({}, { 'GET /web/example.com/apps/wordpress': () => ({ body: webAppForm('wordpress', {}) }) }),
  },
]

/** The control kinds each page has inside its form. */
const kindsOf: Record<string, Kind[]> = {
  AccessKeysPage: ['text'],
  AccountPage: ['text', 'switch', 'select'],
  SshKeysPage: ['text'],
  ExclusionsPages: ['text'],
  CronFormPage: ['text', 'select'],
  'DatabaseFormPage (add)': ['text', 'select'],
  'DatabaseFormPage (edit)': ['text'],
  DnsRecordFormPage: ['text', 'select', 'combobox'],
  'DnsZoneFormPage (add)': ['text', 'select', 'combobox'],
  'DnsZoneFormPage (edit)': ['text', 'checkbox', 'select', 'combobox'],
  MailAccountFormPage: ['text'],
  'MailDomainFormPage (add)': ['text', 'checkbox', 'switch', 'select'],
  'MailDomainFormPage (edit)': ['text', 'checkbox', 'switch', 'select'],
  FirewallBanFormPage: ['text', 'select'],
  FirewallIpsetFormPage: ['text', 'select'],
  FirewallRuleFormPage: ['text', 'select'],
  IpFormPage: ['text', 'checkbox', 'select'],
  ServerConfigPage: ['text', 'checkbox', 'select', 'combobox'],
  ServiceEditPage: ['text', 'checkbox'],
  WhiteLabelPage: ['text', 'checkbox', 'select'],
  PackageFormPage: ['text', 'checkbox', 'select', 'tag'],
  UserEditPage: ['text', 'switch', 'select'],
  UserForm: ['text', 'checkbox', 'switch', 'select'],
  AddWebDomainPage: ['text', 'checkbox', 'select'],
  EditWebDomainPage: ['text', 'checkbox', 'switch', 'select', 'tag'],
  GenerateSslPage: ['text', 'select'],
  SetupWebAppPage: ['text', 'checkbox', 'select'],
}

const enabled = (el: Element) =>
  !el.hasAttribute('disabled') &&
  el.getAttribute('aria-disabled') !== 'true' &&
  !el.hasAttribute('data-disabled') &&
  !el.hasAttribute('readonly') &&
  !el.closest('[hidden], [aria-hidden="true"], fieldset[disabled]')

/** The first enabled control of a kind inside the page's form. */
function firstOf(form: HTMLElement, kind: Kind): HTMLElement | undefined {
  const selector: Record<Kind, string> = {
    text: 'input:not([type]), input[type=text], input[type=email], input[type=number], input[type=password], input[type=url], textarea',
    checkbox: '[role=checkbox]',
    switch: '[role=switch]',
    radio: '[role=radio]:not([aria-checked=true])',
    select: 'button[role=combobox]',
    combobox: 'input[role=combobox]',
    // A TagInput's remove button: the one tag change that types nothing.
    tag: 'button[aria-label^="Remove "]',
  }
  return [...form.querySelectorAll<HTMLElement>(selector[kind])].find(
    (el) => enabled(el) && !(kind === 'text' && el.getAttribute('role') === 'combobox'),
  )
}

function choose(option: HTMLElement) {
  fireEvent.pointerDown(option)
  fireEvent.mouseDown(option)
  fireEvent.pointerUp(option)
  fireEvent.mouseUp(option)
  fireEvent.click(option)
}

/** Changes the control the way a person would. */
async function change(el: HTMLElement, kind: Kind) {
  if (kind === 'text') {
    const input = el as HTMLInputElement
    fireEvent.change(input, { target: { value: input.type === 'number' ? '7' : `${input.value}x` } })
    return
  }
  if (kind === 'select' || kind === 'combobox') {
    fireEvent.click(el)
    if (kind === 'combobox') fireEvent.keyDown(el, { key: 'ArrowDown' })
    const options = await screen.findAllByRole('option')
    const other = options.find((o) => o.getAttribute('aria-selected') !== 'true') ?? options[0]
    choose(other)
    return
  }
  fireEvent.click(el)
}

/** Opens the page and waits for its loaded form (a loading FormPage has no controls). */
async function openForm(page: Page): Promise<{ app: ReturnType<typeof renderApp>; form: HTMLElement }> {
  const app = renderApp(page.path, page.routes())
  let form: HTMLElement | undefined
  await waitFor(
    () => {
      const save = document.querySelector('[data-shortcut="save"]')
      form = save?.closest('form') ?? undefined
      expect(
        form &&
          (['text', 'checkbox', 'switch', 'select', 'combobox', 'radio'] as Kind[]).some((k) => firstOf(form!, k)),
      ).toBe(true)
    },
    { timeout: 5000 },
  )
  return { app, form: form! }
}

async function asksAfter(page: Page, kind: Kind) {
  const { app, form } = await openForm(page)
  const el = firstOf(form, kind)
  expect(el, `no ${kind} on ${page.name}`).toBeTruthy()
  await change(el!, kind)
  fireEvent.click(
    within(document.querySelector('[data-sidebar="sidebar"]') as HTMLElement).getByRole('link', { name: 'Web' }),
  )
  await waitFor(() =>
    expect(
      screen.queryByRole('dialog', { name: 'Unsaved changes' }) !== null || app.location() !== page.path.split('?')[0],
    ).toBe(true),
  )
  expect(screen.queryByRole('dialog', { name: 'Unsaved changes' })).toBeTruthy()
}

const cases = pages.flatMap((page) => kindsOf[page.name].map((kind) => ({ page, kind, label: `${page.name} ${kind}` })))

describe('every form page asks before leaving', () => {
  it('covers every page', () => {
    expect(pages.map((p) => p.name).sort()).toEqual(Object.keys(kindsOf).sort())
  })

  for (const { page, kind, label } of cases) {
    it(`${label}: a changed control asks before leaving through the sidebar`, () => asksAfter(page, kind), 20000)
  }
})

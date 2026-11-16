// Fake mail API for screen tests: builders in the generated wire types, and
// mailRoutes() serving every mail GET with the dev fixtures' data (admin's view):
// example.com (antispam, antivirus, DKIM and Let's Encrypt SSL on, webmail roundcube,
// accounts info and sales; sales forwards and has an autoreply) and example.net
// (suspended, everything off, one suspended account). missing.example answers 404.
import type { MailAccount, MailAccountDetail, MailDomain, MailDomainDetail, MailSSL } from '../api/hestia.gen'
import type { MailAccountView, MailDNSRow, MailDomainView, MailOptions } from '../api/types.gen'
import { signedIn, type Route } from './renderApp'

/** A row of GET /mail; override any field. */
export function mailDomain(name: string, over: Partial<MailDomain> = {}): MailDomain {
  return {
    NAME: name,
    ANTIVIRUS: 'yes',
    ANTISPAM: 'yes',
    REJECT: 'yes',
    RATE_LIMIT: '',
    DKIM: 'yes',
    CATCHALL: '',
    ACCOUNTS: '2',
    U_DISK: '37',
    SSL: 'yes',
    SUSPENDED: 'no',
    TIME: '09:41:07',
    DATE: '2026-03-14',
    WEBMAIL_ALIAS: 'webmail',
    WEBMAIL: 'roundcube',
    ...over,
  }
}

/** One domain as v-list-mail-domain describes it; override any field. */
export function mailDomainDetail(name: string, over: Partial<MailDomainDetail> = {}): MailDomainDetail {
  return {
    NAME: name,
    ANTIVIRUS: 'yes',
    ANTISPAM: 'yes',
    DKIM: 'yes',
    DKIM_SELECTOR: 'mail',
    CATCHALL: '',
    ACCOUNTS: '2',
    RATE_LIMIT: '',
    REJECT: 'yes',
    U_DISK: '37',
    SSL: 'yes',
    SSL_KEY_ALGO: 'ecdsa-256',
    LETSENCRYPT: 'yes',
    SUSPENDED: 'no',
    TIME: '09:41:07',
    DATE: '2026-03-14',
    WEBMAIL_ALIAS: `webmail.${name}`,
    WEBMAIL: 'roundcube',
    U_SMTP_RELAY: 'false',
    U_SMTP_RELAY_HOST: '',
    U_SMTP_RELAY_PORT: '',
    U_SMTP_RELAY_USERNAME: '',
    ...over,
  }
}

/** A row of GET /mail/{domain}/accounts; override any field. */
export function mailAccount(name: string, over: Partial<MailAccount> = {}): MailAccount {
  return {
    NAME: name,
    ALIAS: '',
    FWD: '',
    FWD_ONLY: '',
    AUTOREPLY: 'no',
    QUOTA: '1024',
    U_DISK: '35',
    SUSPENDED: 'no',
    TIME: '09:45:10',
    DATE: '2026-03-14',
    ...over,
  }
}

/** One account as v-list-mail-account describes it; override any field. */
export function mailAccountDetail(
  name: string,
  domain: string,
  over: Partial<MailAccountDetail> = {},
): MailAccountDetail {
  const { NAME: _name, ...row } = mailAccount(name)
  return { NAME: name, ...row, RATE_LIMIT: '', WEBMAIL_ALIAS: `webmail.${domain}`, ...over }
}

/** A Let's Encrypt certificate for mail.<domain>; override any field. */
export function mailSsl(domain: string, over: Partial<MailSSL> = {}): MailSSL {
  return {
    CRT: '-----BEGIN CERTIFICATE-----\nMIIBdevCertificate\n-----END CERTIFICATE-----',
    KEY: '-----BEGIN PRIVATE KEY-----\nMIGHdevKeyNotSecret\n-----END PRIVATE KEY-----',
    CA: '-----BEGIN CERTIFICATE-----\nMIICdevIntermediate\n-----END CERTIFICATE-----',
    SUBJECT: `mail.${domain}`,
    ALIASES: `mail.${domain},webmail.${domain}`,
    NOT_BEFORE: 'Mar 14 09:41:07 2026 GMT',
    NOT_AFTER: 'Jun 12 09:41:06 2026 GMT',
    SIGNATURE: 'ecdsa-with-SHA384',
    PUB_KEY: '256 bit',
    ISSUER: "C=US, O=Let's Encrypt, CN=E6",
    ...over,
  }
}

/** GET /mail/options as the dev server answers an admin; override any field. */
export function mailOptions(over: Partial<MailOptions> = {}): MailOptions {
  return {
    webmailClients: ['roundcube', 'snappymail'],
    webmailAlias: 'webmail',
    antispam: true,
    antivirus: true,
    webmail: true,
    imap: true,
    canSetRate: true,
    adminGate: false,
    keyAlgos: ['ecdsa-256', 'ecdsa-384', 'rsa'],
    ...over,
  }
}

/** The DNS records view for a domain served from ip (checklist section 1.3). */
export function mailDnsRows(
  domain: string,
  ip = '203.0.113.11',
  dkim = 'v=DKIM1; k=rsa; p=MIIBIjANdev...IDAQAB',
): MailDNSRow[] {
  return [
    { record: `mail.${domain}`, type: 'A', priority: '', ttl: '14400', value: ip },
    { record: `webmail.${domain}`, type: 'A', priority: '', ttl: '14400', value: ip },
    { record: domain, type: 'MX', priority: '10', ttl: '14400', value: `mail.${domain}.` },
    { record: domain, type: 'TXT', priority: '', ttl: '14400', value: `v=spf1 a mx ip4:${ip} -all` },
    { record: '_dmarc', type: 'TXT', priority: '', ttl: '14400', value: 'v=DMARC1; p=quarantine; pct=100' },
    { record: 'mail._domainkey', type: 'TXT', priority: '', ttl: '3600', value: dkim },
  ]
}

const notFound = (message: string) => ({ status: 404, body: { error: { code: 'not_found', message } } })

/**
 * Fake mail API routes for renderApp: bootstrap (signedIn(user), mail feature on) plus
 * every mail GET. `overrides` replaces or adds routes by "METHOD /path" key, e.g. the
 * writes a test expects: { 'POST /mail': () => ({ body: { status: 'ok' } }) }.
 */
export function mailRoutes(
  user: Record<string, unknown> = {},
  overrides: Record<string, Route> = {},
): Record<string, Route> {
  const com = mailDomainDetail('example.com')
  const net = mailDomainDetail('example.net', {
    ANTIVIRUS: 'no',
    ANTISPAM: 'no',
    DKIM: 'no',
    DKIM_SELECTOR: '',
    CATCHALL: 'boss@example.net',
    ACCOUNTS: '1',
    RATE_LIMIT: '100',
    REJECT: '',
    U_DISK: '0',
    SSL: 'no',
    SSL_KEY_ALGO: '',
    LETSENCRYPT: 'no',
    SUSPENDED: 'yes',
    TIME: '11:02:55',
    DATE: '2026-04-01',
    WEBMAIL: '',
    U_SMTP_RELAY: '',
  })
  const info = mailAccount('info', { ALIAS: 'contact,hello' })
  const sales = mailAccount('sales', {
    FWD: 'owner@gmail.com,archive@example.org',
    FWD_ONLY: 'yes',
    AUTOREPLY: 'yes',
    QUOTA: 'unlimited',
    U_DISK: '2',
    TIME: '10:12:33',
    DATE: '2026-03-20',
  })
  const contact = mailAccount('contact', { QUOTA: '512', U_DISK: '0', SUSPENDED: 'yes', DATE: '2026-04-01' })
  const view = (domain: MailDomainDetail, ssl?: MailSSL): MailDomainView => (ssl ? { domain, ssl } : { domain })
  const account = (a: MailAccount, domain: string, over: Partial<MailAccountDetail>, msg = ''): MailAccountView => ({
    account: mailAccountDetail(a.NAME, domain, { ...a, ...over }),
    autoreplyMessage: msg,
  })
  const placeholder = 'v=DKIM1; k=rsa; p=DKIM-SUPPORT-IS-NOT-ACTIVATED'
  return {
    'GET /bootstrap': () => ({
      body: signedIn({ features: { dns: true, db: true, mail: true }, ...user }),
    }),
    'GET /mail': () => ({
      body: {
        domains: [
          mailDomain('example.com'),
          mailDomain('example.net', {
            ANTIVIRUS: 'no',
            ANTISPAM: 'no',
            REJECT: '',
            RATE_LIMIT: '100',
            DKIM: 'no',
            CATCHALL: 'boss@example.net',
            ACCOUNTS: '1',
            U_DISK: '0',
            SSL: 'no',
            SUSPENDED: 'yes',
            TIME: '11:02:55',
            DATE: '2026-04-01',
            WEBMAIL: '',
          }),
        ],
        usage: { domains: '2', domainsLimit: 'unlimited', suspended: '1', accounts: '3', accountsLimit: 'unlimited' },
      },
    }),
    'GET /mail/options': () => ({ body: mailOptions() }),
    'GET /mail/example.com': () => ({ body: view(com, mailSsl('example.com')) }),
    'GET /mail/example.net': () => ({ body: view(net) }),
    'GET /mail/example.com/accounts': () => ({ body: { domain: com, accounts: [info, sales] } }),
    'GET /mail/example.net/accounts': () => ({ body: { domain: net, accounts: [contact] } }),
    'GET /mail/example.com/accounts/info': () => ({ body: account(info, 'example.com', {}) }),
    'GET /mail/example.com/accounts/sales': () => ({
      body: account(
        sales,
        'example.com',
        { RATE_LIMIT: '200' },
        'Thanks for your message.\nWe reply within one business day.\nSales team',
      ),
    }),
    'GET /mail/example.net/accounts/contact': () => ({ body: account(contact, 'example.net', {}) }),
    'GET /mail/example.com/dns': () => ({ body: { records: mailDnsRows('example.com') } }),
    'GET /mail/example.net/dns': () => ({
      body: { records: mailDnsRows('example.net', '203.0.113.11', placeholder) },
    }),
    'GET /mail/missing.example': () => notFound("Error: mail domain missing.example doesn't exist"),
    'GET /mail/missing.example/accounts': () => notFound("Error: mail domain missing.example doesn't exist"),
    'GET /mail/missing.example/dns': () => notFound("Error: mail domain missing.example doesn't exist"),
    ...overrides,
  }
}

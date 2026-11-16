// Fake web API for screen tests: builders in the generated wire types, and
// webRoutes() serving every web GET with data representative of the dev fixtures (the
// same users, domains and settings, not byte-identical: logs, certificates and some
// counts are shortened or simplified).
//
// admin (the default): example.com on 203.0.113.10 (Let's Encrypt SSL with ecdsa-256
// and force-HTTPS, aliases www and blog, awstats, FTP admin_dev on /public_html,
// backend PHP-8_2, proxy default) and example.net (suspended, no SSL). bob (pass
// { name: 'bob', role: 'user' }): bakery.example on 10.0.0.5, shown as its NAT
// 198.51.100.7 (custom certificate, redirect to www.bakery.example with 301, custom
// docroot bakery.example/shop, stats login "baker"). missing.example answers 404.
// Writes are not served: pass the ones a test expects in `overrides`.
import type { WebDomain, WebDomainDetail, WebSSL } from '../api/hestia.gen'
import type {
  WebAppField,
  WebAppForm,
  WebAppInfo,
  WebDomains,
  WebDomainView,
  WebLog,
  WebOptions,
} from '../api/types.gen'
import { signedIn, type Route } from './renderApp'

const PROXY_EXT = 'css,htm,html,js,mjs,json,xml,apng,avif,bmp,cur,gif,ico,jfif,jpg,jpeg,png,svg,webp'

/** A row of GET /web (admin's example.com settings under another name); override any field. */
export function webDomain(name: string, over: Partial<WebDomain> = {}): WebDomain {
  return {
    NAME: name,
    IP: '203.0.113.10',
    IP6: '',
    DOCUMENT_ROOT: `/home/admin/web/${name}/public_html/`,
    U_DISK: '128',
    U_BANDWIDTH: '45',
    TPL: 'default',
    ALIAS: `www.${name}`,
    STATS: 'awstats',
    STATS_USER: '',
    SSL: 'yes',
    SSL_HOME: 'same',
    LETSENCRYPT: 'yes',
    FTP_USER: '',
    FTP_PATH: '',
    AUTH_USER: '',
    BACKEND: 'PHP-8_2',
    PROXY: 'default',
    PROXY_EXT,
    SUSPENDED: 'no',
    TIME: '09:41:07',
    DATE: '2026-03-02',
    ...over,
  }
}

/** One domain as v-list-web-domain describes it; override any field. */
export function webDomainDetail(name: string, over: Partial<WebDomainDetail> = {}): WebDomainDetail {
  const { NAME, IP, IP6, DOCUMENT_ROOT, U_DISK, U_BANDWIDTH, TPL, ALIAS, STATS, STATS_USER, SSL, SSL_HOME } =
    webDomain(name)
  return {
    NAME,
    IP,
    IP6,
    DOCUMENT_ROOT,
    U_DISK,
    U_BANDWIDTH,
    TPL,
    ALIAS,
    STATS,
    STATS_USER,
    SSL,
    SSL_FORCE: 'yes',
    SSL_HSTS: 'no',
    SSL_HOME,
    SSL_KEY_ALGO: 'ecdsa-256',
    LETSENCRYPT_FAIL_COUNT: '0',
    LETSENCRYPT: 'yes',
    FTP_USER: '',
    FTP_PATH: '',
    AUTH_USER: '',
    BACKEND: 'PHP-8_2',
    PROXY: 'default',
    PROXY_EXT,
    FASTCGI_CACHE: '',
    FASTCGI_DURATION: '',
    REDIRECT: '',
    REDIRECT_CODE: '',
    CUSTOM_DOCROOT: '',
    SUSPENDED: 'no',
    TIME: '09:41:07',
    DATE: '2026-03-02',
    ...over,
  }
}

/** A Let's Encrypt certificate for <domain>; override any field. KEY is a dev value. */
export function webSsl(domain: string, over: Partial<WebSSL> = {}): WebSSL {
  return {
    CRT: '-----BEGIN CERTIFICATE-----\nMIIFdevLetsEncryptLeaf\n-----END CERTIFICATE-----',
    KEY: '-----BEGIN PRIVATE KEY-----\nMIGHdevKeyNotSecret\n-----END PRIVATE KEY-----',
    CA: '-----BEGIN CERTIFICATE-----\nMIIEdevLetsEncryptE7\n-----END CERTIFICATE-----',
    SUBJECT: domain,
    ALIASES: `${domain},www.${domain}`,
    NOT_BEFORE: 'Mar  2 09:41:10 2026 GMT',
    NOT_AFTER: 'May 31 09:41:09 2026 GMT',
    SIGNATURE: 'ecdsa-with-SHA384',
    PUB_KEY: '256 bit',
    ISSUER: "C = US, O = Let's Encrypt, CN = E7",
    ...over,
  }
}

/** GET /web/options as the dev server answers admin viewing admin; override any field. */
export function webOptions(over: Partial<WebOptions> = {}): WebOptions {
  return {
    ips: [
      { ip: '203.0.113.10', display: '203.0.113.10' },
      { ip: '203.0.113.11', display: '203.0.113.11' },
    ],
    webSystem: 'apache2',
    webBackend: 'php-fpm',
    proxySystem: 'nginx',
    nginx: false,
    purge: true,
    ftp: true,
    statsTypes: ['none', 'awstats'],
    dns: true,
    mail: true,
    appInstaller: true,
    canEditTemplates: true,
    templates: ['default', 'hosting-php', 'wordpress'],
    backendTemplates: ['default', 'PHP-8_2', 'PHP-8_3'],
    proxyTemplates: ['caching', 'default', 'hosting'],
    defaultProxyExt: PROXY_EXT.split(','),
    canRebuild: true,
    adminGate: true,
    ftpPrefix: 'admin_',
    webRoot: '/home/admin/web/',
    keyAlgos: ['ecdsa-256', 'ecdsa-384', 'rsa'],
    ...over,
  }
}

/**
 * GET /web/{domain} for a detail, derived the simple way (aliases, FTP pairs, key type,
 * redirect against www.<domain>); override any field (docroot and displayIp, say).
 */
export function webDomainView(
  domain: WebDomainDetail,
  over: Partial<WebDomainView> = {},
  ssl: WebSSL | undefined = domain.SSL === 'no' ? undefined : webSsl(domain.NAME),
): WebDomainView {
  const split = (s: string, sep: string) => (s === '' ? [] : s.split(sep))
  const paths = split(domain.FTP_PATH, ':')
  const algo = domain.SSL_KEY_ALGO.toLowerCase()
  const target = domain.REDIRECT
  return {
    domain,
    ...(ssl ? { ssl } : {}),
    aliases: split(domain.ALIAS, ','),
    displayIp: domain.IP,
    keyAlgo: ['ecdsa-256', 'ecdsa-384', 'rsa'].includes(algo) ? algo : domain.SSL === 'yes' ? 'rsa' : 'ecdsa-256',
    ftp: split(domain.FTP_USER, ':').map((full, i) => ({
      name: full.slice(full.indexOf('_') + 1),
      path: paths[i] ?? '',
    })),
    docroot: { enabled: domain.CUSTOM_DOCROOT !== '', domain: '', folder: '' },
    redirect: {
      enabled: target !== '',
      mode: target === `www.${domain.NAME}` ? 'www' : target === domain.NAME ? 'bare' : 'custom',
      target,
      code: domain.REDIRECT_CODE || '301',
    },
    ...over,
  }
}

/** The installer catalogue in checklist order: id, name, group, version, supported PHP. */
const catalogue: [string, string, string, string, string[]][] = [
  ['castopod', 'Castopod', 'podcasting', '1.15.5', ['8.1', '8.2', '8.3', '8.4', '8.5']],
  ['classicpress', 'ClassicPress', 'cms', 'latest', ['7.4', '8.0', '8.1', '8.2', '8.3', '8.4', '8.5']],
  ['concretecms', 'ConcreteCMS', 'cms', 'latest', ['7.4', '8.0', '8.1', '8.2', '8.3', '8.4', '8.5']],
  ['dokuwiki', 'DokuWiki', 'wiki', 'latest', ['8.0', '8.1', '8.2', '8.3', '8.4']],
  ['dolibarr', 'Dolibarr', 'crm', '24.0.1', ['8.1', '8.2', '8.3', '8.4']],
  ['drupal', 'Drupal', 'cms', 'latest', ['8.1', '8.2', '8.3', '8.4', '8.5']],
  ['flarum', 'Flarum', 'forum', 'latest', ['8.2', '8.3']],
  ['grav', 'Grav', 'cms', '2.0.24', ['8.3', '8.4', '8.5']],
  ['joomla', 'Joomla', 'cms', '6.1.3', ['8.3', '8.4']],
  ['laravel', 'Laravel', 'framework', 'latest', ['8.3', '8.4', '8.5']],
  ['mediawiki', 'MediaWiki', 'cms', '1.46.0', ['8.2', '8.3', '8.4', '8.5']],
  ['namelessmc', 'NamelessMC', 'cms', '2.2.5', ['7.4', '8.0', '8.1', '8.2']],
  ['nextcloud', 'Nextcloud', 'cloud', 'latest', ['8.2', '8.3', '8.4', '8.5']],
  ['opencart', 'OpenCart', 'ecommerce', '4.1.0.4', ['8.0', '8.1', '8.2', '8.3', '8.4', '8.5']],
  ['prestashop', 'PrestaShop', 'ecommerce', '9.1.5', ['8.1', '8.2', '8.3', '8.4', '8.5']],
  ['qloapps', 'QloApps', 'ecommerce', '1.7.0', ['8.1', '8.2', '8.3', '8.4']],
  ['shopware', 'Shopware', 'ecommerce', 'latest', ['8.2', '8.3', '8.4', '8.5']],
  ['symfony', 'Symfony', 'framework', 'latest', ['8.4', '8.5']],
  ['thirtybees', 'ThirtyBees', 'ecommerce', '1.7.0', ['7.4', '8.0', '8.1', '8.2', '8.3']],
  ['vvveb', 'Vvveb', 'cms', 'latest', ['7.4', '8.0', '8.1', '8.2', '8.3', '8.4']],
  ['wordpress', 'WordPress', 'cms', 'latest', ['7.4', '8.0', '8.1', '8.2', '8.3', '8.4', '8.5']],
  ['osticket', 'osTicket', 'helpdesk', '1.18.4', ['8.2', '8.3', '8.4']],
]
/** v-list-sys-php in the dev fixtures. */
const installedPhp = ['8.2', '8.3']
const phpFor = (id: string) =>
  (catalogue.find((a) => a[0] === id)?.[4] ?? installedPhp).filter((v) => installedPhp.includes(v))

/** One installer card; installable follows the dev PHP versions (Symfony is not). Override any field. */
export function webApp(id: string, over: Partial<WebAppInfo> = {}): WebAppInfo {
  const [, name, group, version] = catalogue.find((a) => a[0] === id) ?? [id, id, 'cms', 'latest', []]
  return { id, name, group, version, installable: phpFor(id).length > 0, ...over }
}

const field = (key: string, label: string, type: WebAppField['type'], value = ''): WebAppField => ({
  key,
  label,
  type,
  value,
  placeholder: '',
  options: [],
})

/** GET /web/{domain}/apps/{app}: WordPress's form for wordpress, an admin login otherwise. Override any field. */
export function webAppForm(id: string, over: Partial<WebAppForm> = {}): WebAppForm {
  const fields =
    id === 'wordpress'
      ? [
          field('site_name', 'Site Name', 'text', 'WordPress Blog'),
          field('username', 'Username', 'text', 'wpadmin'),
          field('email', 'Email', 'text'),
          field('password', 'Password', 'password'),
          {
            ...field('language', 'Language', 'select', 'en_US'),
            options: [
              { value: 'de_DE', label: 'German' },
              { value: 'en_US', label: 'English' },
              { value: 'fr_FR', label: 'French' },
            ],
          },
        ]
      : [
          field('username', 'Username', 'text', 'admin'),
          field('password', 'Password', 'password'),
          field('email', 'Email', 'text'),
        ]
  return {
    app: webApp(id),
    fields,
    phpVersions: phpFor(id),
    database: { hosts: ['localhost'], name: `${id.slice(0, 4)}_d3v9` },
    docrootClean: true,
    ...over,
  }
}

const notFound = (message: string) => ({ status: 404, body: { error: { code: 'not_found', message } } })

const log = (type: 'access' | 'error', lines: string[]): WebLog => ({ type, lines })

/**
 * Fake web API routes for renderApp: bootstrap (signedIn(user), every feature on) plus
 * every web GET for the dev user it names (admin unless user.name is 'bob'). `overrides`
 * replaces or adds routes by "METHOD /path" key, e.g. the writes a test expects:
 * { 'POST /web': () => ({ body: { status: 'ok' } }) }.
 */
export function webRoutes(
  user: Record<string, unknown> = {},
  overrides: Record<string, Route> = {},
): Record<string, Route> {
  const bob = user.name === 'bob'
  const routes: Record<string, Route> = {
    'GET /bootstrap': () => ({ body: signedIn(user) }),
  }
  let list: WebDomains
  let options: WebOptions
  const views: WebDomainView[] = []
  const logs: Record<string, [string[], string[]]> = {}
  if (bob) {
    const bake = webDomainDetail('bakery.example', {
      IP: '10.0.0.5',
      DOCUMENT_ROOT: '/home/bob/web/bakery.example/public_html/shop/',
      U_DISK: '312',
      U_BANDWIDTH: '2048',
      STATS_USER: 'baker',
      SSL_FORCE: 'no',
      SSL_KEY_ALGO: '',
      LETSENCRYPT: 'no',
      BACKEND: 'PHP-8_3',
      REDIRECT: 'www.bakery.example',
      REDIRECT_CODE: '301',
      CUSTOM_DOCROOT: '/home/bob/web/bakery.example/public_html/shop/',
      TIME: '14:20:31',
      DATE: '2026-05-11',
    })
    views.push(
      webDomainView(
        bake,
        { displayIp: '198.51.100.7', docroot: { enabled: true, domain: 'bakery.example', folder: 'shop' } },
        webSsl('bakery.example', {
          CRT: '-----BEGIN CERTIFICATE-----\nMIIGdevBakeryLeaf\n-----END CERTIFICATE-----',
          KEY: '-----BEGIN RSA PRIVATE KEY-----\nMIIEdevBakeryKeyNotSecret\n-----END RSA PRIVATE KEY-----',
          CA: '-----BEGIN CERTIFICATE-----\nMIIGdevSectigoIntermediate\n-----END CERTIFICATE-----',
          NOT_BEFORE: 'Jan 15 00:00:00 2026 GMT',
          NOT_AFTER: 'Jan 14 23:59:59 2027 GMT',
          SIGNATURE: 'sha256WithRSAEncryption',
          PUB_KEY: '2048 bit',
          ISSUER:
            'C = GB, ST = Greater Manchester, L = Salford, O = Sectigo Limited, CN = Sectigo RSA Domain Validation Secure Server CA',
        }),
      ),
    )
    list = {
      domains: [
        webDomain('bakery.example', {
          IP: bake.IP,
          DOCUMENT_ROOT: bake.DOCUMENT_ROOT,
          U_DISK: bake.U_DISK,
          U_BANDWIDTH: bake.U_BANDWIDTH,
          STATS_USER: bake.STATS_USER,
          LETSENCRYPT: bake.LETSENCRYPT,
          BACKEND: bake.BACKEND,
          TIME: bake.TIME,
          DATE: bake.DATE,
        }),
      ],
      displayIps: { '10.0.0.5': '198.51.100.7' },
      usage: { domains: '1', domainsLimit: '3', suspended: '0', aliases: '1', aliasesLimit: '15' },
    }
    options = webOptions({
      ips: [
        { ip: '10.0.0.5', display: '198.51.100.7' },
        { ip: '203.0.113.10', display: '203.0.113.10' },
      ],
      canEditTemplates: false,
      templates: [],
      backendTemplates: [],
      proxyTemplates: [],
      canRebuild: false,
      adminGate: false,
      ftpPrefix: 'bob_',
      webRoot: '/home/bob/web/',
    })
    logs['bakery.example'] = [
      [
        '198.51.100.40 - - [21/Oct/2026:08:02:11 +0000] "GET /shop/ HTTP/2.0" 200 9311 "-" "Mozilla/5.0"',
        '198.51.100.40 - - [21/Oct/2026:08:02:15 +0000] "GET /shop/cakes HTTP/2.0" 200 7740 "-" "Mozilla/5.0"',
      ],
      [],
    ]
  } else {
    const com = webDomainDetail('example.com', {
      ALIAS: 'www.example.com,blog.example.com',
      FTP_USER: 'admin_dev',
      FTP_PATH: '/public_html',
    })
    const net = webDomainDetail('example.net', {
      U_DISK: '0',
      U_BANDWIDTH: '0',
      ALIAS: '',
      STATS: '',
      SSL: 'no',
      SSL_FORCE: 'no',
      SSL_KEY_ALGO: '',
      LETSENCRYPT: 'no',
      SUSPENDED: 'yes',
      TIME: '11:02:55',
      DATE: '2026-04-01',
    })
    views.push(
      webDomainView(com, {}, webSsl('example.com', { ALIASES: 'example.com,blog.example.com,www.example.com' })),
      webDomainView(net),
    )
    list = {
      domains: [
        webDomain('example.com', { ALIAS: com.ALIAS, FTP_USER: com.FTP_USER, FTP_PATH: com.FTP_PATH }),
        webDomain('example.net', {
          U_DISK: '0',
          U_BANDWIDTH: '0',
          ALIAS: '',
          STATS: '',
          SSL: 'no',
          LETSENCRYPT: 'no',
          SUSPENDED: 'yes',
          TIME: '11:02:55',
          DATE: '2026-04-01',
        }),
      ],
      displayIps: { '203.0.113.10': '203.0.113.10' },
      usage: { domains: '2', domainsLimit: 'unlimited', suspended: '1', aliases: '2', aliasesLimit: 'unlimited' },
    }
    options = webOptions()
    logs['example.com'] = [
      [
        '198.51.100.23 - - [21/Oct/2026:09:12:01 +0000] "GET / HTTP/2.0" 200 5124 "-" "Mozilla/5.0"',
        '198.51.100.23 - - [21/Oct/2026:09:12:02 +0000] "GET /css/site.css HTTP/2.0" 200 1830 "-" "Mozilla/5.0"',
        '203.0.113.77 - - [21/Oct/2026:09:14:45 +0000] "GET /blog/ HTTP/1.1" 301 162 "-" "curl/8.5.0"',
      ],
      ['[Tue Oct 21 09:20:13.402114 2026] [core:info] [client 192.0.2.14:51544] AH00128: File does not exist'],
    ]
    logs['example.net'] = [[], []]
  }
  routes['GET /web'] = () => ({ body: list })
  routes['GET /web/options'] = () => ({ body: options })
  const apps = catalogue.map(([id]) => webApp(id))
  for (const view of views) {
    const d = view.domain.NAME
    const [access, errors] = logs[d]
    routes[`GET /web/${d}`] = () => ({ body: view })
    routes[`GET /web/${d}/logs?type=access`] = () => ({ body: log('access', access) })
    routes[`GET /web/${d}/logs?type=error`] = () => ({ body: log('error', errors) })
    routes[`GET /web/${d}/apps`] = () => ({ body: { apps } })
    for (const app of apps) routes[`GET /web/${d}/apps/${app.id}`] = () => ({ body: webAppForm(app.id) })
  }
  const missing = "Error: web domain missing.example doesn't exist"
  for (const path of ['', '/logs?type=access', '/logs?type=error', '/apps']) {
    routes[`GET /web/missing.example${path}`] = () => notFound(missing)
  }
  return { ...routes, ...overrides }
}

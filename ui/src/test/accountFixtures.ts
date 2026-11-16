// Fake account-management API for screen tests (Plan 3): one builder per Hestia type
// (in the generated wire types, overridable), and route maps for renderApp serving every
// Plan 3 GET with the dev fixtures' data, as the server would answer admin (ROOT_USER):
//   users admin (root), bob (2FA on), susp (suspended), ops (non-root admin);
//   packages starter-plus, starter, default, system;
//   admin: 2 cron jobs, 2 backups, 2 snapshots, 2 SSH keys, 3 access keys,
//   2 notifications, 5 log rows, 3 auth-log rows; the system log; 3 months of stats.
// Each map takes the bootstrap user's overrides (signedIn) and route overrides by
// "METHOD /path" key, which replace or add routes (e.g. the writes a test expects).
// A path with a query string is its own key: 'GET /logs?user=system'.
import type {
  AccessKey,
  APIInfo,
  AuthLogEntry,
  Backup,
  BackupExclusions,
  CronJob,
  LogEntry,
  Notification,
  Package,
  PackageEntry,
  Snapshot,
  SnapshotDetail,
  SnapshotNode,
  SSHKey,
  UserDetail,
  UserEntry,
} from '../api/hestia.gen'
import type { AccountOptions, AccountView, PackageOptions, StatsMonth } from '../api/types.gen'
import { anonymous, signedIn, type Route } from './renderApp'

type Routes = Record<string, Route>

const notFound = (message: string) => ({ status: 404, body: { error: { code: 'not_found', message } } })
const ok =
  (body: unknown = { status: 'ok' }) =>
  () => ({ body })
const enc = encodeURIComponent

// ---------------------------------------------------------------- builders

/** A row of GET /users; override any field. */
export function userEntry(user: string, over: Partial<UserEntry> = {}): UserEntry {
  return {
    USER: user,
    NAME: 'Bob Baker',
    PACKAGE: 'default',
    DISK_QUOTA: 'unlimited',
    BANDWIDTH: 'unlimited',
    CONTACT: `${user}@acme-bakery.com`,
    ROLE: 'user',
    SUSPENDED: 'no',
    IP_OWNED: '0',
    U_DISK: '512',
    U_BANDWIDTH: '640',
    U_WEB_DOMAINS: '1',
    U_DNS_DOMAINS: '1',
    U_MAIL_DOMAINS: '1',
    U_DATABASES: '1',
    U_BACKUPS: '1',
    LANGUAGE: 'en',
    TIME: '12:00:00',
    DATE: '2026-01-10',
    ...over,
  }
}

/** One account as GET /account and GET /users/{user} describe it; override any field. */
export function userDetail(user: string, over: Partial<UserDetail> = {}): UserDetail {
  return {
    USER: user,
    NAME: 'Bob Baker',
    PACKAGE: 'default',
    NS: 'ns1.acme-hosting.net,ns2.acme-hosting.net',
    SHELL: 'nologin',
    CONTACT: `${user}@acme-bakery.com`,
    ROLE: 'user',
    SUSPENDED: 'no',
    LANGUAGE: 'en',
    THEME: '',
    PREF_UI_SORT: 'name',
    LOGIN_DISABLED: 'no',
    LOGIN_USE_IPLIST: 'no',
    LOGIN_ALLOW_IPS: '',
    PHPCLI: '',
    TIME: '12:00:00',
    DATE: '2026-01-10',
    DISK_QUOTA: 'unlimited',
    BANDWIDTH: 'unlimited',
    WEB_DOMAINS: '3',
    DNS_DOMAINS: '5',
    MAIL_DOMAINS: '1',
    DATABASES: '1',
    CRON_JOBS: '1',
    BACKUPS: '1',
    ...over,
  }
}

/** A package as GET /packages/{package} describes it; override any field. */
export function pkg(name: string, over: Partial<Package> = {}): Package {
  return {
    NAME: name,
    WEB_TEMPLATE: 'default',
    BACKEND_TEMPLATE: 'default',
    PROXY_TEMPLATE: 'default',
    DNS_TEMPLATE: 'default',
    WEB_DOMAINS: '1',
    WEB_ALIASES: '5',
    DNS_DOMAINS: '1',
    DNS_RECORDS: 'unlimited',
    MAIL_DOMAINS: '1',
    MAIL_ACCOUNTS: '5',
    RATE_LIMIT: '200',
    DATABASES: '1',
    CRON_JOBS: '1',
    DISK_QUOTA: '1000',
    CPU_QUOTA: 'unlimited',
    CPU_QUOTA_PERIOD: 'unlimited',
    MEMORY_LIMIT: 'unlimited',
    SWAP_LIMIT: 'unlimited',
    BANDWIDTH: '1000',
    NS: 'ns1.example.tld,ns2.example.tld',
    SHELL: 'nologin',
    BACKUPS: '1',
    BACKUPS_INCREMENTAL: 'no',
    TIME: '11:30:02',
    DATE: '2026-04-01',
    ...over,
  }
}

/** The row GET /packages lists for a package (no backend, rate limit or incremental). */
export function packageEntry(p: Package): PackageEntry {
  const { BACKEND_TEMPLATE: _b, RATE_LIMIT: _r, BACKUPS_INCREMENTAL: _i, ...row } = p
  return row
}

/** A cron job; override any field. */
export function cronJob(id: string, over: Partial<CronJob> = {}): CronJob {
  return {
    MIN: '*/15',
    HOUR: '*',
    DAY: '*',
    MONTH: '*',
    WDAY: '*',
    CMD: 'php /home/admin/web/example.com/public_html/cron.php >/dev/null 2>&1',
    JOB: id,
    SUSPENDED: 'no',
    TIME: '06:12:44',
    DATE: '2026-03-02',
    ...over,
  }
}

/** A backup; override any field. */
export function backup(name: string, over: Partial<Backup> = {}): Backup {
  return {
    NAME: name,
    TYPE: 'local',
    SIZE: '312',
    WEB: 'example.com,shop.example.com',
    DNS: 'example.com',
    MAIL: 'example.com',
    DB: 'admin_wp,admin_shop',
    CRON: '2',
    UDIR: 'backup,.ssh',
    RUNTIME: '2',
    TIME: '03:02:11',
    DATE: '2026-10-19',
    ...over,
  }
}

/** A restic snapshot; override any field. */
export function snapshot(shortId: string, over: Partial<Snapshot> = {}): Snapshot {
  return {
    short_id: shortId,
    id: shortId.padEnd(64, '0'),
    time: '2026-10-19T03:00:04.211873519+02:00',
    hostname: 'server1.acme-hosting.net',
    paths: ['/home/admin'],
    ...over,
  }
}

/** An SSH key; override any field. */
export function sshKey(id: string, over: Partial<SSHKey> = {}): SSHKey {
  return { ID: id, KEY: 'SHA256:Nl3rD1x0C0m8nQ8mZ7HbVd1uQ0pXy4F2tWgJ5eKsLhA', ...over }
}

/** An access key (never with its secret); override any field. */
export function accessKey(id: string, over: Partial<AccessKey> = {}): AccessKey {
  return {
    ACCESS_KEY_ID: id,
    USER: 'admin',
    PERMISSIONS: ['mail-accounts'],
    COMMENT: '',
    TIME: '09:41:07',
    DATE: '2026-03-14',
    ...over,
  }
}

/** A panel notification; override any field. */
export function notification(id: string, over: Partial<Notification> = {}): Notification {
  return {
    ID: id,
    TOPIC: 'Backup failed',
    NOTICE: '<p>Backup of <b>bob</b> could not be uploaded to the SFTP host.</p>',
    TYPE: 'error',
    ACK: 'no',
    PRIORITY: '1',
    TIME: '03:05:12',
    DATE: '2026-10-20',
    ...over,
  }
}

/** A user-log row; override any field. */
export function logEntry(id: string, over: Partial<LogEntry> = {}): LogEntry {
  return {
    ID: id,
    DATE: '2026-10-18',
    TIME: '09:12:44',
    LEVEL: 'Info',
    CATEGORY: 'System',
    MESSAGE: 'Password changed.',
    ...over,
  }
}

/** An auth-log row (no session ID); override any field. */
export function authLogEntry(id: string, over: Partial<AuthLogEntry> = {}): AuthLogEntry {
  return {
    ID: id,
    DATE: '2026-10-20',
    TIME: '09:40:55',
    IP: '203.0.113.5',
    ACTION: 'login',
    STATUS: 'success',
    USER_AGENT: 'Mozilla/5.0 (X11; Linux x86_64) Firefox/133.0',
    ...over,
  }
}

/** One month of GET /stats (MB; quotas may be "unlimited"); override any field. */
export function statsMonth(date: string, over: Partial<StatsMonth> = {}): StatsMonth {
  return {
    date,
    time: '12:00:00',
    package: 'system',
    ipOwned: '1',
    diskQuota: 'unlimited',
    uDisk: '7342',
    uDiskDirs: '312',
    uDiskWeb: '3671',
    uDiskMail: '1024',
    uDiskDb: '886',
    bandwidth: 'unlimited',
    uBandwidth: '38211',
    uWebDomains: '2',
    uWebSsl: '1',
    uWebAliases: '2',
    uDnsDomains: '2',
    uDnsRecords: '13',
    uMailDomains: '2',
    uMailDkim: '1',
    uMailAccounts: '3',
    uDatabases: '2',
    uCronJobs: '2',
    uBackups: '2',
    uUsers: '',
    ...over,
  }
}

// ---------------------------------------------------------------- dev data

const bootstrap =
  (user: Record<string, unknown>): Route =>
  () => ({ body: signedIn(user) })

export const devUsers: UserEntry[] = [
  userEntry('admin', {
    NAME: 'System Administrator',
    PACKAGE: 'system',
    CONTACT: 'admin@acme-hosting.net',
    ROLE: 'admin',
    IP_OWNED: '1',
    U_DISK: '7342',
    U_BANDWIDTH: '38211',
    U_WEB_DOMAINS: '2',
    U_DNS_DOMAINS: '2',
    U_MAIL_DOMAINS: '2',
    U_DATABASES: '2',
    U_BACKUPS: '2',
  }),
  userEntry('bob', { DISK_QUOTA: '1000', BANDWIDTH: '1000' }),
  userEntry('susp', {
    NAME: 'Suspended Customer',
    CONTACT: 'ops@mariposa-studio.net',
    SUSPENDED: 'yes',
    U_DISK: '0',
    U_BANDWIDTH: '0',
    U_WEB_DOMAINS: '0',
    U_DNS_DOMAINS: '0',
    U_MAIL_DOMAINS: '0',
    U_DATABASES: '0',
    U_BACKUPS: '0',
  }),
  userEntry('ops', {
    NAME: 'Operations Admin',
    PACKAGE: 'system',
    CONTACT: 'ops@acme-hosting.net',
    ROLE: 'admin',
    U_DISK: '0',
    U_BANDWIDTH: '0',
    U_WEB_DOMAINS: '0',
    U_DNS_DOMAINS: '0',
    U_MAIL_DOMAINS: '0',
    U_DATABASES: '0',
    U_BACKUPS: '0',
  }),
]

const U = 'unlimited'
export const devPackages: Package[] = [
  pkg('starter-plus', {
    WEB_DOMAINS: '3',
    WEB_ALIASES: '10',
    DNS_DOMAINS: '3',
    MAIL_DOMAINS: '3',
    MAIL_ACCOUNTS: '10',
    DATABASES: '3',
    CRON_JOBS: '5',
    DISK_QUOTA: '5000',
    BANDWIDTH: '5000',
    BACKUPS: '3',
    TIME: '09:15:00',
    DATE: '2026-06-02',
  }),
  pkg('starter'),
  pkg('default', {
    WEB_DOMAINS: U,
    WEB_ALIASES: U,
    DNS_DOMAINS: U,
    MAIL_DOMAINS: U,
    MAIL_ACCOUNTS: U,
    DATABASES: U,
    CRON_JOBS: U,
    DISK_QUOTA: U,
    BANDWIDTH: U,
    NS: 'ns1.domain.tld,ns2.domain.tld',
    TIME: '18:00:00',
    DATE: '2019-01-15',
  }),
  pkg('system', {
    WEB_ALIASES: '1',
    MAIL_ACCOUNTS: '1',
    DATABASES: '0',
    CRON_JOBS: U,
    DISK_QUOTA: U,
    BANDWIDTH: U,
    NS: 'ns1.domain.tld,ns2.domain.tld',
    BACKUPS_INCREMENTAL: 'yes',
    TIME: '00:00:00',
    DATE: '2022-01-20',
  }),
]

/** GET /packages/options as the dev server answers; override any field. */
export function packageOptions(over: Partial<PackageOptions> = {}): PackageOptions {
  return {
    webTemplates: ['default', 'hosting-php', 'wordpress'],
    backendTemplates: ['default', 'PHP-8_2', 'PHP-8_3'],
    proxyTemplates: ['caching', 'default', 'hosting'],
    dnsTemplates: ['default', 'child-ns', 'gmail'],
    shells: ['bash', 'dash', 'nologin', 'rbash', 'sh', 'zsh'],
    webSystem: 'apache2',
    webBackend: 'php-fpm',
    proxySystem: 'nginx',
    dnsSystem: 'bind9',
    resourcesLimit: false,
    ...over,
  }
}

/** Users and packages: GET /users, /packages, /packages/options and each package. */
export function usersRoutes(user: Record<string, unknown> = {}, overrides: Routes = {}): Routes {
  const routes: Routes = {
    'GET /bootstrap': bootstrap(user),
    'GET /users': ok({ users: devUsers, rootUser: 'admin', self: 'admin' }),
    'GET /packages': ok({ packages: devPackages.map(packageEntry) }),
    'GET /packages/options': ok(packageOptions()),
    'GET /packages/missing': () => notFound("Error: package missing doesn't exist"),
  }
  // Like the server, which never opens the system package.
  for (const p of devPackages) if (p.NAME !== 'system') routes[`GET /packages/${p.NAME}`] = ok(p)
  return { ...routes, ...overrides }
}

/** An AccountView of the dev data; override the user's fields. */
export function accountView(name: string, over: Partial<AccountView> = {}): AccountView {
  const row = devUsers.find((u) => u.USER === name) ?? userEntry(name)
  const admin = row.ROLE === 'admin'
  return {
    user: userDetail(name, {
      NAME: row.NAME,
      PACKAGE: row.PACKAGE,
      CONTACT: row.CONTACT,
      ROLE: row.ROLE,
      SUSPENDED: row.SUSPENDED,
      SHELL: admin ? 'bash' : 'nologin',
    }),
    twofa: name === 'bob',
    self: name === 'admin',
    editable: { details: true, theme: true, admin: true },
    ...over,
  }
}

/** GET /account/options as the dev server answers an admin; override any field. */
export function accountOptions(over: Partial<AccountOptions> = {}): AccountOptions {
  return {
    languages: ['en', 'de', 'fr'],
    themes: ['dark', 'default', 'flat', 'vestia'],
    packages: devPackages.map((p) => p.NAME),
    shells: ['bash', 'dash', 'nologin', 'rbash', 'sh', 'zsh'],
    phpVersions: ['8.2', '8.3'],
    defaultPhp: '8.3',
    dnsSystem: true,
    apiAccess: true,
    logsVisible: true,
    systemLanguage: 'en',
    ...over,
  }
}

/** Profile and user editing: GET /account, /account/options and /users/{user}. */
export function profileRoutes(user: Record<string, unknown> = {}, overrides: Routes = {}): Routes {
  const routes: Routes = {
    'GET /bootstrap': bootstrap(user),
    'GET /account': ok(accountView('admin')),
    'GET /account/options': ok(accountOptions()),
    'GET /users/missing': () => notFound("Error: user missing doesn't exist"),
  }
  for (const u of devUsers) routes[`GET /users/${u.USER}`] = ok(accountView(u.USER, { self: u.USER === 'admin' }))
  return { ...routes, ...overrides }
}

/** Account recovery: the anonymous bootstrap and the four POSTs, all succeeding. */
export function recoveryRoutes(boot: Record<string, unknown> = {}, overrides: Routes = {}): Routes {
  return {
    'GET /bootstrap': () => ({ body: { ...anonymous, ...boot } }),
    'POST /auth/reset/request': ok({ status: 'sent' }),
    'POST /auth/reset/confirm': ok(),
    'POST /auth/reset-2fa/request': ok({ status: 'sent' }),
    'POST /auth/reset-2fa/confirm': ok(),
    ...overrides,
  }
}

export const devAccessKeys: AccessKey[] = [
  accessKey('aB3dEfGh1JkLmN0pQrSt', { PERMISSIONS: ['billing', 'mail-accounts'], COMMENT: 'Billing sync' }),
  accessKey('Zx9yWv8uTs7rQp6oNm5l', { PERMISSIONS: [], TIME: '10:02:55', DATE: '2026-03-15' }),
  accessKey('Kp4Lm7Nq2Rs5Tv8Wx1Yz', {
    PERMISSIONS: ['purge-nginx-cache'],
    COMMENT: 'CI deploy',
    TIME: '16:20:00',
    DATE: '2026-09-01',
  }),
]

export const devApis: APIInfo[] = [
  { Name: 'billing', COMMANDS: ['v-add-user', 'v-delete-user', 'v-list-users'], ROLE: 'admin' },
  { Name: 'mail-accounts', COMMANDS: ['v-list-mail-domains', 'v-add-mail-account'], ROLE: 'user' },
  { Name: 'purge-nginx-cache', COMMANDS: ['v-purge-nginx-cache', 'v-list-web-domains'], ROLE: 'user' },
]

/** Admin's user log, newest first (as GET /logs answers). */
export const devLog: LogEntry[] = [
  logEntry('41', {
    DATE: '2026-10-20',
    TIME: '04:15:40',
    LEVEL: 'Error',
    CATEGORY: 'Backup',
    MESSAGE: 'Backup upload to SFTP host failed.',
  }),
  logEntry('40', {
    DATE: '2026-10-19',
    TIME: '03:02:11',
    CATEGORY: 'Backup',
    MESSAGE: 'Backup created (admin.2026-10-19_03-00-02.tar).',
  }),
  logEntry('39', { LEVEL: 'Warning', CATEGORY: 'Security' }),
  logEntry('38', {
    DATE: '2026-10-17',
    TIME: '14:20:05',
    CATEGORY: 'DNS',
    MESSAGE: 'Added DNS record (www) to example.com.',
  }),
  logEntry('37', {
    DATE: '2026-10-16',
    TIME: '10:02:11',
    CATEGORY: 'Web',
    MESSAGE: 'Added new web domain (example.com).',
  }),
]

export const devSystemLog: LogEntry[] = [
  logEntry('203', {
    DATE: '2026-10-21',
    TIME: '06:10:30',
    LEVEL: 'Warning',
    CATEGORY: 'Firewall',
    MESSAGE: 'Banned IP address 198.51.100.23 (ssh).',
  }),
  logEntry('202', {
    DATE: '2026-10-20',
    TIME: '02:00:00',
    CATEGORY: 'Updates',
    MESSAGE: 'System packages are up to date.',
  }),
  logEntry('201', { DATE: '2026-10-19', TIME: '00:20:03', MESSAGE: 'Usage statistics updated.' }),
]

/** Keys, notifications and logs: every GET of Slice D but statistics. */
export function keysRoutes(user: Record<string, unknown> = {}, overrides: Routes = {}): Routes {
  const routes: Routes = {
    'GET /bootstrap': bootstrap(user),
    'GET /account/ssh-keys': ok({
      keys: [
        sshKey('admin@workstation'),
        sshKey('deploy key 2024', { KEY: 'SHA256:eU2u7tqL0ZkQ4yW3d8p0n5bXr1sHcVjMf6aTgOiBzKo' }),
      ],
    }),
    'GET /account/access-keys': ok({ keys: devAccessKeys, apis: devApis }),
    'GET /account/access-keys/missing': () => notFound("Error: access key missing doesn't exist"),
    'GET /notifications': ok({
      items: [
        notification('2'),
        notification('1', {
          TOPIC: 'Welcome to Hostisan',
          NOTICE:
            "<p>Your panel is ready. Read the <a href='https://docs.example.org/'>documentation</a> to get started.</p>",
          TYPE: 'info',
          ACK: 'yes',
          PRIORITY: '0',
          TIME: '12:00:00',
          DATE: '2026-01-10',
        }),
      ],
    }),
    'GET /logs': ok({ entries: devLog, user: 'admin' }),
    'GET /logs?user=admin': ok({ entries: devLog, user: 'admin' }),
    'GET /logs?user=system': ok({ entries: devSystemLog, user: 'system' }),
    'GET /logs?user=bob': ok({
      entries: [
        logEntry('1', {
          DATE: '2026-10-18',
          TIME: '11:00:00',
          CATEGORY: 'Mail',
          MESSAGE: 'Added mail account info@acme-bakery.com.',
        }),
      ],
      user: 'bob',
    }),
    'GET /logs/auth': ok({
      entries: [
        authLogEntry('3', {
          DATE: '2026-10-21',
          TIME: '08:15:02',
          USER_AGENT: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) Safari/605.1.15',
        }),
        authLogEntry('2'),
        authLogEntry('1', {
          DATE: '2026-10-19',
          TIME: '08:59:12',
          IP: '198.51.100.23',
          STATUS: 'failed',
          USER_AGENT: 'curl/8.5.0',
        }),
      ],
      user: 'admin',
    }),
  }
  for (const k of devAccessKeys) routes[`GET /account/access-keys/${k.ACCESS_KEY_ID}`] = ok(k)
  return { ...routes, ...overrides }
}

/** Statistics: overall (admin, no ?user=) and per user for admin and bob, newest first. */
export function statsRoutes(user: Record<string, unknown> = {}, overrides: Routes = {}): Routes {
  const users = ['admin', 'bob', 'susp', 'ops']
  const admin = [
    statsMonth('2026-10-31'),
    statsMonth('2026-09-30', { uDisk: '7120', uDiskWeb: '3560', uBandwidth: '35400' }),
    statsMonth('2026-08-31', { uDisk: '6900', uDiskWeb: '3450', uBandwidth: '30120' }),
  ]
  const bobMonth = (date: string, over: Partial<StatsMonth>) =>
    statsMonth(date, {
      package: 'default',
      diskQuota: '1000',
      bandwidth: '1000',
      uWebDomains: '1',
      uDnsDomains: '1',
      uMailDomains: '1',
      uDatabases: '1',
      uCronJobs: '1',
      uBackups: '1',
      ...over,
    })
  const bob = [
    bobMonth('2026-10-31', { uDisk: '512', uDiskWeb: '256', uBandwidth: '640' }),
    bobMonth('2026-09-30', { uDisk: '455', uDiskWeb: '227', uBandwidth: '1310', bandwidth: 'unlimited' }),
    bobMonth('2026-08-31', { uDisk: '410', uDiskWeb: '205', uBandwidth: '220' }),
  ]
  const overall = (date: string, over: Partial<StatsMonth>) =>
    statsMonth(date, { package: 'default', diskQuota: '0', bandwidth: '0', uUsers: '4', ...over })
  return {
    'GET /bootstrap': bootstrap(user),
    'GET /stats': ok({
      months: [
        overall('2026-10-31', { uDisk: '7854', uDiskWeb: '3927', uBandwidth: '38851' }),
        overall('2026-09-30', { uDisk: '7575', uDiskWeb: '3787', uBandwidth: '36710' }),
        overall('2026-08-31', { uDisk: '7310', uDiskWeb: '3655', uBandwidth: '30340' }),
      ],
      overall: true,
      user: '',
      users,
    }),
    'GET /stats?user=admin': ok({ months: admin, overall: false, user: 'admin', users }),
    'GET /stats?user=bob': ok({ months: bob, overall: false, user: 'bob', users }),
    ...overrides,
  }
}

/** Cron: admin's two jobs (job 2 suspended), reports on, the server's time zone. */
export function cronRoutes(user: Record<string, unknown> = {}, overrides: Routes = {}): Routes {
  const one = cronJob('1')
  const two = cronJob('2', {
    MIN: '30',
    HOUR: '3',
    WDAY: '1,2,3,4,5',
    CMD: 'cd /home/admin/web/example.com/public_html && php artisan schedule:run >> /dev/null 2>&1',
    SUSPENDED: 'yes',
    TIME: '14:01:09',
    DATE: '2026-04-18',
  })
  return {
    'GET /bootstrap': bootstrap(user),
    'GET /cron': ok({ jobs: [one, two], reports: true, usage: { jobs: '2', limit: 'unlimited', suspended: '1' }, contact: 'admin@acme-hosting.net' }),
    'GET /cron/options': ok({ timezone: 'UTC', now: '2026-10-21T12:00:00Z' }),
    'GET /cron/1': ok(one),
    'GET /cron/2': ok(two),
    'GET /cron/99': () => notFound("Error: cron job 99 doesn't exist"),
    ...overrides,
  }
}

export const devBackups: Backup[] = [
  backup('admin.2026-10-20_03-00-01.tar', {
    TYPE: 'local,sftp',
    SIZE: '1243',
    DNS: '',
    UDIR: '',
    RUNTIME: '75',
    TIME: '04:15:40',
    DATE: '2026-10-20',
  }),
  backup('admin.2026-10-19_03-00-02.tar'),
]

export const devExclusions: BackupExclusions = {
  WEB: [
    { Key: 'example.com', Value: 'public_html/cache,public_html/tmp' },
    { Key: 'shop.example.com', Value: '' },
  ],
  DNS: [{ Key: 'example.org', Value: '' }],
  MAIL: [{ Key: 'example.com', Value: 'info,support' }],
  DB: [{ Key: 'admin_wp', Value: '' }],
  CRON: [{ Key: '*', Value: '' }],
  USER: [
    { Key: 'Downloads', Value: '' },
    { Key: '.cache', Value: '' },
  ],
}

export const devSnapshots: Snapshot[] = [
  snapshot('e41f0a29', {
    id: 'e41f0a29bc7712ab34cd56ef7890ab12cd34ef567890ab12cd34ef567890ab12',
    time: '2026-10-20T03:00:05.532190021+02:00',
  }),
  snapshot('3c9b8a7e', { id: '3c9b8a7e5d1f2a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3' }),
]

const snapshotDetail = (date: string, time: string): SnapshotDetail => ({
  TYPE: 'restic',
  WEB: 'example.com,shop.example.com',
  DNS: 'example.com',
  MAIL: 'example.com',
  DB: 'admin_wp',
  CRON: '2',
  UDIR: 'backup,.ssh',
  RUNTIME: '1',
  TIME: time,
  DATE: date,
})

const node = (name: string, type: string, path: string, ctime: string, size = 0): SnapshotNode => ({
  name,
  type,
  path,
  size,
  ctime,
})

/**
 * Backups for user (default admin's view; pass bootstrap overrides): two backups, the
 * exclusions, two snapshots and the file browser at /home/admin and its Documents.
 */
export function backupRoutes(user: Record<string, unknown> = {}, overrides: Routes = {}): Routes {
  const routes: Routes = {
    'GET /bootstrap': bootstrap(user),
    'GET /backup': ok({ backups: devBackups, usage: { backups: '2', limit: '10' }, incremental: true }),
    'GET /backup/missing': () => notFound("Error: backup missing doesn't exist"),
    'GET /backup/exclusions': ok({
      exclusions: devExclusions,
      text: {
        web: 'example.com:public_html/cache:public_html/tmp\nshop.example.com',
        mail: 'example.com:info:support',
        db: 'admin_wp',
        userdir: 'Downloads\n.cache',
      },
    }),
    'GET /backup/incremental': ok({ snapshots: devSnapshots }),
  }
  for (const b of devBackups) routes[`GET /backup/${b.NAME}`] = ok({ backup: b })
  for (const s of devSnapshots) {
    const base = `GET /backup/incremental/${s.short_id}`
    routes[base] = ok({ id: s.short_id, detail: snapshotDetail(s.time.slice(0, 10), s.time.slice(11, 19)) })
    routes[`${base}/files?path=${enc('/home/admin')}`] = ok({
      path: '/home/admin',
      parent: '',
      nodes: [
        node('conf', 'dir', '/home/admin/conf', '2026-01-10T12:00:00+01:00'),
        node('Documents', 'dir', '/home/admin/Documents', '2026-09-01T10:00:00+02:00'),
        node('web', 'dir', '/home/admin/web', '2026-01-10T12:00:00+01:00'),
        node('.bash_history', 'file', '/home/admin/.bash_history', '2026-10-18T22:01:13+02:00', 5120),
        node('notes.txt', 'file', '/home/admin/notes.txt', '2026-10-18T09:12:44.1+02:00', 2048),
        node('current', 'symlink', '/home/admin/current', '2026-02-02T08:00:00+01:00'),
      ],
    })
    routes[`${base}/files?path=${enc('/home/admin/Documents')}`] = ok({
      path: '/home/admin/Documents',
      parent: '/home/admin',
      nodes: [
        node('invoice-2026-09.pdf', 'file', '/home/admin/Documents/invoice-2026-09.pdf', '2026-09-30T18:00:00+02:00', 88231),
        node('todo.md', 'file', '/home/admin/Documents/todo.md', '2026-10-02T09:30:00+02:00', 412),
      ],
    })
  }
  return { ...routes, ...overrides }
}

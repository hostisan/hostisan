// Fake server-admin API for screen tests: builders in the generated wire types, and
// serverRoutes() serving every server GET with the Go dev fixtures' data (admin's
// view): server1.acme-hosting.net with apache2 + nginx, php8.2-fpm (running) and
// php8.3-fpm (stopped), the 10 default firewall rules (2 = HESTIA 8083, 10 = SSH 22),
// three IPs (10.0.0.5 with NAT 198.51.100.7), 203.0.113.9 banned in SSH and MAIL,
// one IP set, and every job id answering jobDone().
import type { FirewallBan, FirewallIpset, SysInfo } from '../api/hestia.gen'
import type {
  EditorView,
  FirewallRuleRow,
  GraphData,
  IPRow,
  JobStatus,
  ServiceRow,
  SettingsView,
} from '../api/types.gen'
import { signedIn, type Route } from './renderApp'

/** The job id every serverRoutes() job route answers. */
export const JOB_ID = '0123456789abcdef0123456789abcdef'

/** v-list-sys-info as the API sends it; override any field. */
export function sysInfo(over: Partial<SysInfo> = {}): SysInfo {
  return {
    HOSTNAME: 'server1.acme-hosting.net',
    OS: 'Debian',
    VERSION: '12.8',
    ARCH: 'x86_64',
    HESTIA: '1.10.5',
    RELEASE: 'release',
    UPTIME: '41763',
    LOADAVERAGE: '0.42 / 0.51 / 0.47',
    ...over,
  }
}

/** A row of GET /server/services; a running service with an editor of its own name. */
export function service(name: string, over: Partial<ServiceRow> = {}): ServiceRow {
  return {
    name,
    system: 'web server',
    state: 'running',
    cpu: '0.2',
    mem: '58',
    rtime: '5143',
    editor: name,
    ...over,
  }
}

/** The dev services list, sorted by name. */
export function services(): ServiceRow[] {
  return [
    service('apache2'),
    service('bind9', { system: 'dns server', cpu: '0.0', mem: '44' }),
    service('clamav-daemon', { system: 'email anti-virus', cpu: '0.4', mem: '1021' }),
    service('cron', { system: 'job scheduler', cpu: '0', mem: '1' }),
    service('dovecot', { system: 'imap/pop3 server', cpu: '0', mem: '14' }),
    service('exim4', { system: 'mail server', cpu: '0', mem: '9' }),
    service('fail2ban', { system: 'brute-force protection', cpu: '0.1', mem: '27' }),
    service('iptables', { system: 'firewall', cpu: '0', mem: '0', rtime: '0', editor: '' }),
    service('mariadb', { system: 'database server', cpu: '0.5', mem: '187' }),
    service('nginx', { system: 'reverse proxy', cpu: '0.1', mem: '12' }),
    service('php8.2-fpm', { system: 'php interpreter', cpu: '0', mem: '31' }),
    service('php8.3-fpm', { system: 'php interpreter', state: 'stopped', cpu: '0', mem: '0', rtime: '0' }),
    service('spamassassin', { system: 'email spam filter', cpu: '0', mem: '212' }),
    service('ssh', { system: 'ssh server', cpu: '0', mem: '4' }),
    service('vsftpd', { system: 'ftp server', state: 'stopped', cpu: '0', mem: '0', rtime: '0' }),
    service('web-terminal', { system: 'web terminal backend', cpu: '0', mem: '35', editor: '' }),
  ]
}

/** The nginx editor (basic mode available). */
export function editorView(over: Partial<EditorView> = {}): EditorView {
  return {
    service: 'nginx',
    title: 'NGINX',
    family: 'nginx',
    restart: true,
    readOnly: false,
    syntaxCheck: true,
    directives: { worker_processes: 'auto', worker_connections: '1024', client_max_body_size: '256m' },
    files: [
      {
        id: 'nginx',
        path: '/etc/nginx/nginx.conf',
        content: 'worker_processes auto;\nevents {\n\tworker_connections 1024;\n}\n',
        hash: 'a'.repeat(64),
      },
    ],
    ...over,
  }
}

/** GET /server/settings with the dev config; override any field. */
export function settingsView(over: Partial<SettingsView> = {}): SettingsView {
  const php = (
    version: string,
    installed: boolean,
    protectedV = false,
    usedBy: SettingsView['phpVersions'][0]['usedBy'] = [],
  ) => ({
    version,
    installed,
    protected: protectedV,
    usedBy,
  })
  return {
    hostname: 'server1.acme-hosting.net',
    timezone: 'UTC',
    theme: 'dark',
    themeForAll: false,
    language: 'en',
    debugMode: false,
    version: '1.10.5',
    releaseBranch: 'release',
    previewFeatures: false,
    upgradeSendEmail: true,
    upgradeSendEmailLog: false,
    proxySystem: 'nginx',
    webSystem: 'apache2',
    webBackend: 'php-fpm',
    phpVersions: [
      php('5.6', false),
      php('7.4', false),
      php('8.1', false),
      php('8.2', true, true, [{ user: 'admin', domain: 'example.com' }]),
      php('8.3', true, false),
      php('8.4', false),
    ],
    defaultPhp: '8.2',
    dnsSystem: 'bind9',
    dnsCluster: [],
    dnsStatus: true,
    mail: {
      system: 'exim4',
      antivirus: 'clamav-daemon',
      antispam: 'spamassassin',
      webmailSystem: 'roundcube,snappymail',
      webmailAlias: 'webmail',
      relay: false,
      relayHost: '',
      relayPort: '',
      relayUser: '',
    },
    db: {
      system: 'mysql,pgsql',
      mysql: true,
      pgsql: true,
      mysqlLocal: true,
      hosts: [
        { type: 'mysql', host: 'localhost', maxDb: '500', current: '2' },
        { type: 'pgsql', host: 'localhost', maxDb: '500', current: '1' },
      ],
      pmaAlias: 'phpmyadmin',
      pgaAlias: 'phppgadmin',
      pmaSso: true,
      pmaRestrict: false,
    },
    backup: {
      local: true,
      mode: 'zstd',
      level: '4',
      directory: '/backup',
      remote: {
        type: 'sftp',
        host: 'backup.acme-hosting.net',
        port: '22',
        username: 'backup',
        path: '/srv/backups/server1',
        bucket: '',
        keyId: '',
      },
      incremental: true,
      restic: {
        repo: 'sftp:restic@backup.acme-hosting.net:/srv/restic/server1',
        snapshots: '10',
        keepDaily: '7',
        keepWeekly: '4',
        keepMonthly: '6',
        keepYearly: '1',
      },
    },
    ssl: {
      crt: '-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----',
      ca: '',
      subject: 'server1.acme-hosting.net',
      aliases: 'server1.acme-hosting.net',
      notBefore: 'Oct 17 09:41:12 2026 GMT',
      notAfter: 'Jan 15 09:41:11 2027 GMT',
      signature: 'ecdsa-with-SHA256',
      pubKey: '256 bit',
      issuer: "C = US, O = Let's Encrypt, CN = E5",
    },
    security: {
      apiSystem: '2',
      legacyApi: false,
      apiAllowedIp: 'allow-all\n',
      loginStyle: 'default',
      passwordReset: true,
      sessionTimeout: '60',
      csrfStrictness: '1',
    },
    policies: {
      editDetails: true,
      editWebTemplates: true,
      editDnsTemplates: true,
      viewLogs: true,
      deleteLogs: true,
      viewSuspended: false,
      backupSuspendedUsers: false,
      syncErrorDocuments: true,
      syncSkeleton: true,
      enforceSubdomain: true,
    },
    plugins: {
      appInstaller: true,
      fileManager: true,
      webTerminal: true,
      resourcesLimit: false,
      diskQuota: false,
      firewall: true,
    },
    options: {
      timezones: ['Europe/Berlin', 'UTC'],
      themes: ['dark', 'default', 'flat', 'vestia'],
      languages: ['en', 'de'],
    },
    protection: { protectedAdmin: false, hideAdmin: false, hideServices: false },
    canEditProtection: true,
    ...over,
  }
}

/** A firewall rule as GET /server/firewall lists it; override any field. */
export function firewallRule(id: string, over: Partial<FirewallRuleRow> = {}): FirewallRuleRow {
  return {
    id,
    action: 'ACCEPT',
    protocol: 'TCP',
    port: '80,443',
    ip: '0.0.0.0/0',
    comment: 'WEB',
    suspended: false,
    protected: '',
    expect: `expect-${id}`,
    time: '12:00:00',
    date: '2026-01-10',
    ...over,
  }
}

/** The 10 default rules, in evaluation order (highest id first). */
export function firewallRules(): FirewallRuleRow[] {
  const r = (id: string, protocol: string, port: string, comment: string, protectedBy = '') =>
    firewallRule(id, { protocol, port, comment, protected: protectedBy })
  return [
    r('10', 'TCP', '22', 'SSH', 'ssh'),
    r('9', 'TCP', '80,443', 'WEB'),
    r('8', 'TCP', '21,12000-12100', 'FTP'),
    r('7', 'UDP', '53', 'DNS'),
    r('6', 'TCP', '25,465,587', 'SMTP'),
    r('5', 'TCP', '110,995', 'POP3'),
    r('4', 'TCP', '143,993', 'IMAP'),
    r('3', 'TCP', '3306,5432', 'DB'),
    r('2', 'TCP', '8083', 'HESTIA', 'panel'),
    r('1', 'ICMP', '0', 'PING'),
  ]
}

/** An IP row; override any field. Display follows NAT unless overridden. */
export function ipRow(ip: string, over: Partial<IPRow> = {}): IPRow {
  const row: IPRow = {
    ip,
    owner: 'admin',
    status: 'shared',
    name: '',
    users: 'admin',
    webDomains: '0',
    interface: 'eth0',
    netmask: '255.255.255.0',
    nat: '',
    display: ip,
    time: '12:00:00',
    date: '2026-01-10',
    ...over,
  }
  if (over.display === undefined) row.display = row.nat || row.ip
  return row
}

/** The dev IPs (shared fixture v-list-sys-ips). */
export function ipRows(): IPRow[] {
  return [
    ipRow('10.0.0.5', {
      owner: 'bob',
      status: 'dedicated',
      users: 'bob',
      webDomains: '1',
      interface: 'eth1',
      nat: '198.51.100.7',
    }),
    ipRow('203.0.113.10', { name: 'main', webDomains: '2' }),
    ipRow('203.0.113.11', { status: 'dedicated', name: 'secondary' }),
  ]
}

function job(state: string, over: Partial<JobStatus> = {}): JobStatus {
  return {
    id: JOB_ID,
    title: 'Server task',
    state,
    step: 'Done',
    steps: ['Done'],
    error: null,
    result: null,
    started: '2026-10-21T12:00:00Z',
    finished: state === 'running' ? '' : '2026-10-21T12:00:03Z',
    ...over,
  }
}

/** A running job at step (steps so far default to [step]). */
export function jobRunning(step = 'Working', steps: string[] = [step]): JobStatus {
  return job('running', { step, steps })
}

/** A finished job with its result. */
export function jobDone(result: unknown = { status: 'ok' }, steps: string[] = ['Done']): JobStatus {
  return job('done', { result, steps, step: steps[steps.length - 1] ?? '' })
}

/** A failed job: msg (and its args) as the API error, failing at the last step. */
export function jobFailed(
  msg: string,
  steps: string[] = ['Failed step'],
  code = 'command_failed',
  args?: string[],
): JobStatus {
  return job('failed', {
    error: { code, message: msg, ...(args ? { args } : {}) },
    steps,
    step: steps[steps.length - 1] ?? '',
  })
}

const series = (cols: number): GraphData['series'] =>
  Array.from({ length: 12 }, (_, i) =>
    i === 11 ? Array(cols).fill(null) : Array.from({ length: cols }, (_, k) => 10 + i + k),
  )

/**
 * Every server GET with the dev data, the bootstrap of a root admin with every
 * feature, and GET /server/jobs/JOB_ID answering jobDone(). overrides replace or add
 * routes by key ("METHOD /path").
 */
export function serverRoutes(overrides: Record<string, Route> = {}): Record<string, Route> {
  const bans: FirewallBan[] = [
    { IP: '203.0.113.9', CHAIN: 'MAIL', TIME: '04:22:41', DATE: '2026-10-20' },
    { IP: '203.0.113.9', CHAIN: 'SSH', TIME: '03:15:09', DATE: '2026-10-20' },
    { IP: '198.51.100.0/24', CHAIN: 'WEB', TIME: '08:01:12', DATE: '2026-10-19' },
  ]
  const ipset: FirewallIpset = {
    LISTNAME: 'blacklist',
    IP_VERSION: 'v4',
    AUTOUPDATE: 'yes',
    SUSPENDED: 'no',
    SOURCE: 'script:/usr/local/hestia/install/common/firewall/ipset/blacklist.sh',
    TIME: '04:00:30',
    DATE: '2026-10-21',
  }
  const charts = [
    { id: 'la', type: 'la', title: 'Load Average', unit: 'Load', legend: ['Load', 'Processes'] },
    { id: 'mem', type: 'mem', title: 'Memory Usage', unit: 'MB', legend: ['Used', 'Swap', 'Free'] },
    {
      id: 'net_eth0',
      type: 'net',
      title: 'Bandwidth Usage eth0',
      unit: 'Bytes/s',
      legend: ['Input (rx)', 'Output (tx)'],
    },
    { id: 'nginx', type: 'web', title: 'NGINX Usage', unit: 'Connections', legend: ['Connections'] },
    { id: 'mail', type: 'mail', title: 'Exim Usage', unit: 'Emails in queue', legend: ['Emails'] },
    {
      id: 'mysql_localhost',
      type: 'db',
      title: 'MySQL Usage on localhost',
      unit: 'Queries/s',
      legend: ['Queries', 'Slow'],
    },
    { id: 'ssh', type: 'ssh', title: 'SSH Usage', unit: 'Connections', legend: ['Connections'] },
  ]
  const routes: Record<string, Route> = {
    'GET /bootstrap': () => ({ body: signedIn({}) }),
    'GET /server/services': () => ({
      body: { info: sysInfo(), release: 'release', services: services(), firewall: true },
    }),
    'GET /server/editors/nginx': () => ({ body: editorView() }),
    'GET /server/settings': () => ({ body: settingsView() }),
    'GET /server/graphs': () => ({ body: { charts: charts.map(({ id, type, title }) => ({ id, type, title })) } }),
    'GET /server/updates': () => ({
      body: {
        packages: [
          {
            name: 'hestia',
            displayName: 'hostisan',
            description: 'Hostisan core package',
            version: '1.10.5',
            arch: 'amd64',
            updated: true,
          },
          {
            name: 'hestia-php',
            displayName: 'hostisan-php',
            description: 'Hostisan internal php interpreter',
            version: '8.3.14',
            arch: 'amd64',
            updated: false,
          },
          {
            name: 'hestia-nginx',
            displayName: 'hostisan-nginx',
            description: 'Hostisan internal web server',
            version: '1.27.4',
            arch: 'amd64',
            updated: true,
          },
        ],
        autoupdate: true,
      },
    }),
    'GET /server/white-label': () => ({
      body: {
        appName: 'Hostisan',
        title: '{{page}} - {{hostname}} - {{appname}}',
        fromName: '',
        fromEmail: '',
        subjectEmail: '{{subject}}',
        hideDocs: false,
        hostname: 'server1.acme-hosting.net',
      },
    }),
    'GET /server/preview': () => ({
      body: {
        rows: [
          {
            category: 'System',
            name: 'Policy: Allow suspended users to log in with read-only access',
            status: 'Partially implemented',
          },
        ],
      },
    }),
    'GET /server/ips': () => ({ body: { ips: ipRows(), sortOrder: 'name' } }),
    'GET /server/ips/options': () => ({
      body: { interfaces: ['eth0', 'eth1'], users: ['admin', 'bob', 'susp', 'ops'], rootUser: 'admin' },
    }),
    'GET /server/firewall': () => ({ body: { enabled: true, bans: true, rules: firewallRules() } }),
    'GET /server/firewall/bans': () => ({ body: { rows: bans } }),
    'GET /server/firewall/ipsets': () => ({ body: { enabled: true, rows: [ipset] } }),
    [`GET /server/jobs/${JOB_ID}`]: () => ({ body: jobDone() }),
  }
  for (const kind of ['cpu', 'mem', 'disk', 'net', 'web', 'dns', 'mail', 'db']) {
    routes[`GET /server/status/${kind}`] = () => ({ body: { kind, text: `${kind} status` } })
  }
  for (const ip of ipRows()) routes[`GET /server/ips/${ip.ip}`] = () => ({ body: ip })
  for (const rule of firewallRules()) routes[`GET /server/firewall/rules/${rule.id}`] = () => ({ body: rule })
  for (const c of charts) {
    for (const period of ['daily', 'weekly', 'monthly', 'yearly', 'biennially', 'triennially']) {
      routes[`GET /server/graphs/${c.id}?period=${period}`] = () => ({
        body: {
          id: c.id,
          period,
          start: 1760964000,
          step: 7200,
          unit: c.unit,
          legend: c.legend,
          series: series(c.legend.length),
        },
      })
    }
  }
  return { ...routes, ...overrides }
}

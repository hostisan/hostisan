/**
 * Text the untranslated-text audit (src/test/i18nAudit.ts) accepts as it is: product
 * names, protocol and record codes, and punctuation. Matched against the whole
 * trimmed text; text without letters passes anyway. Each Plan 7 track appends only
 * under its own heading below.
 */
export const allowUntranslated: ReadonlySet<string> = new Set([
  // Task 0: product names
  'Hostisan',
  'phpMyAdmin',
  'phpPgAdmin',
  "Let's Encrypt",
  'MySQL',
  'MariaDB',
  'PostgreSQL',
  'NGINX',
  'Apache',
  'Exim',
  'Dovecot',
  'SpamAssassin',
  'ClamAV',
  'Roundcube',
  'SnappyMail',
  'Fail2Ban',
  'Composer',
  'Node.js',
  'Cloudflare',
  // Task 0: protocols, standards and codes
  'DKIM',
  'DMARC',
  'SPF',
  'SSL',
  'TLS',
  'PHP',
  'PHP-FPM',
  'DNS',
  'DNSSEC',
  'FTP',
  'SFTP',
  'SSH',
  'HTTP',
  'HTTPS',
  'HSTS',
  'IMAP',
  'POP3',
  'SMTP',
  'IP',
  'IPv4',
  'IPv6',
  'TCP',
  'UDP',
  'ICMP',
  'URL',
  'API',
  'TTL',
  'SOA',
  'A',
  'AAAA',
  'CAA',
  'CNAME',
  'DS',
  'DNSKEY',
  'MX',
  'NS',
  'PTR',
  'SRV',
  'TXT',
  'UTF-8',
  'CSV',
  'JSON',
  // Task 0: punctuation and separators
  '·',
  '—',
  '–',
  '/',
  '|',
  ':',
  '…',
  '%',
  '@',

  // Track A

  // Track B1
  // DNS: example values in placeholders
  'acme-bakery.com',
  'www',
  // web: example values in placeholders
  'https://acme-bakery.com/shop',
  'owner@acme-bakery.com',
  'hostmaster@acme-bakery.com',
  'staging',
  'webp',
  // web: product names
  "SSL · Let's Encrypt",
  // mail: protocols and ports
  'SSL/TLS',
  'STARTTLS',
  '993 SSL/TLS',
  '995 SSL/TLS',
  '465 SSL/TLS, 587 STARTTLS',
  // mail: example values and PEM placeholders
  'info',
  'hello',
  'orders@northwind-shop.io',
  'marta@northwind-shop.io',
  'smtp.postmarkapp.com',
  '-----BEGIN CERTIFICATE-----',
  '-----BEGIN PRIVATE KEY-----',

  // Track B2
  // db: example placeholder
  'ops@northwind-shop.io',
  // cron: example placeholder
  'php /home/admin/web/northwind-shop.io/private/artisan schedule:run',
  // users: example placeholders
  'business',
  'acme',
  'Acme Bakery',
  'owner@acme-bakery.com',
  // backup: example paths and a product name
  'domain.tld:public_html/cache:public_html/tmp',
  'domain.tld:info:support:postmaster',
  'Restic',
  // account: example placeholders and a key path
  'WHMCS billing integration',
  '~/.ssh/id_ed25519.pub',
  'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA… ops@acme-hosting.net',

  // Track B3
  // firewall actions (iptables targets)
  'ACCEPT',
  'DROP',
  // example placeholders
  'acme-bakery.com',
  'country-cn',
  'https://',
  'server1-backups',
  'r2',
  '/server1',
  'webmail',
  '.example.com',
  // backup and compression product names
  'Backblaze',
  'Rclone',
  'gzip',
  'zstd',

  // Track B4
  'H', // the logo mark on the sign-in screens (designs/auth/AuthLayout.tsx)

  // Track C
  'H', // the sidebar's logo mark
  'esc', // the palette footer's key cap

  // Track D
  'ns1.acme-hosting.net',
])

import { describe, expect, it } from 'vitest'
import {
  axisTimeFormat,
  formatBytesPerSecond,
  formatServiceRole,
  formatUsage,
  humanizeMinutes,
  humanizeUsage,
  sentenceCase,
  stepText,
} from './format'

describe('humanizeMinutes (humanize_time, SYS §6.5 and SVC §1.1)', () => {
  it.each([
    ['0', '0 minutes'],
    ['1', '1 minute'],
    ['59', '59 minutes'],
    ['60', '1 hour'],
    ['89', '1 hour'],
    ['90', '2 hours'],
    ['1409', '23 hours'],
    ['1410', '1 day'],
    ['5421', '4 days'],
    ['41763', '29 days'],
  ])('%s minutes -> %s', (input, want) => {
    expect(humanizeMinutes(input, (m) => m)).toBe(want)
  })

  it('shows a dash for a missing value', () => {
    expect(humanizeMinutes('', (m) => m)).toBe('-')
    expect(humanizeMinutes('abc', (m) => m)).toBe('-')
  })

  it('translates the unit msgid', () => {
    const t = (m: string) => ({ '%d minutes': '%d Minuten', '%d day': '%d Tag' })[m] ?? m
    expect(humanizeMinutes('5', t)).toBe('5 Minuten')
    expect(humanizeMinutes('1440', t)).toBe('1 Tag')
  })
})

describe('humanizeUsage (humanize_usage_size/measure, SYS §6.5)', () => {
  it.each([
    ['0', '0.00', 'mb'],
    ['0.4', '0.00', 'mb'],
    ['512', '512.00', 'mb'],
    ['999', '999.00', 'mb'],
    ['1000', '0.98', 'gb'],
    ['1024', '1.00', 'gb'],
    ['2048', '2.00', 'gb'],
    ['1048576', '1.00', 'tb'],
    ['5000000', '4.77', 'tb'],
    ['1073741824', '1.00', 'pb'],
    ['unlimited', '∞', ''],
  ])('%s MB -> %s %s', (input, size, measure) => {
    expect(humanizeUsage(input)).toEqual({ size, measure })
  })

  it('uses a thousands separator and the rounding asked for', () => {
    expect(humanizeUsage(String(1024 ** 3 * 1500))).toEqual({ size: '1,500.00', measure: 'pb' })
    expect(humanizeUsage('2048', 0)).toEqual({ size: '2', measure: 'gb' })
  })

  it('formatUsage joins size and measure', () => {
    expect(formatUsage('2048')).toBe('2.00 gb')
    expect(formatUsage('unlimited')).toBe('∞')
  })
})

describe('formatBytesPerSecond (decision 23: net graphs are bytes/s)', () => {
  it.each([
    [0, '0 B/s'],
    [512, '512 B/s'],
    [2048, '2.0 KB/s'],
    [182000, '177.7 KB/s'],
    [5 * 1024 * 1024, '5.0 MB/s'],
    [3 * 1024 ** 3, '3.0 GB/s'],
  ])('%d -> %s', (input, want) => {
    expect(formatBytesPerSecond(input)).toBe(want)
  })
})

describe('formatServiceRole', () => {
  it('sentence-cases the core role and keeps acronyms', () => {
    expect(formatServiceRole('php interpreter')).toBe('PHP interpreter')
    expect(formatServiceRole('imap/pop3 server')).toBe('IMAP/POP3 server')
    expect(formatServiceRole('web server')).toBe('Web server')
  })
})

describe('stepText', () => {
  const fr: Record<string, string> = {
    'Install PHP %s': 'Installer PHP %s',
    'Change hostname': 'Changer le nom',
    'Check %s': 'Vérifier %s',
    'Restart %s': 'Redémarrer %s',
    'Restart server': 'Redémarrer le serveur',
    'Check services': 'Vérifier les services',
    'Save %s configuration': 'Enregistrer la configuration de %s',
    'Save %s': 'Enregistrer %s',
  }
  const t = (s: string, ...args: Array<string | number>) => {
    const text = fr[s] ?? s
    return args.reduce<string>((acc, a) => acc.replace('%s', String(a)), text)
  }
  it('translates PHP steps with the version as the argument', () => {
    expect(stepText('Install PHP 8.4', t)).toBe('Installer PHP 8.4')
    expect(stepText('Remove PHP 8.3', t)).toBe('Remove PHP 8.3')
  })
  it('translates other steps as msgids', () => {
    expect(stepText('Change hostname', t)).toBe('Changer le nom')
  })
  it('tries the whole step as a msgid before the patterns', () => {
    expect(stepText('Restart server', t)).toBe('Redémarrer le serveur')
    expect(stepText('Check services', t)).toBe('Vérifier les services')
  })
  it('fills a pattern msgid with the matched name', () => {
    expect(stepText('Check nginx', t)).toBe('Vérifier nginx')
    expect(stepText('Restart php8.2-fpm', t)).toBe('Redémarrer php8.2-fpm')
  })
  it('prefers the most specific pattern', () => {
    expect(stepText('Save nginx configuration', t)).toBe('Enregistrer la configuration de nginx')
    expect(stepText('Save nginx', t)).toBe('Enregistrer nginx')
  })
  it('leaves an unknown step as it is', () => {
    expect(stepText('Something else', t)).toBe('Something else')
  })
})

describe('sentenceCase', () => {
  it('lowercases title words but keeps names', () => {
    expect(sentenceCase('Memory Usage')).toBe('Memory usage')
    expect(sentenceCase('NGINX Usage on MySQL')).toBe('NGINX usage on MySQL')
  })
})

describe('axisTimeFormat', () => {
  const v = Date.UTC(2025, 2, 14, 9, 30)
  it('uses the browser locale and time zone', () => {
    expect(axisTimeFormat('daily')(v)).toBe(
      new Date(v).toLocaleString(undefined, { hour: '2-digit', minute: '2-digit' }),
    )
    expect(axisTimeFormat('weekly')(v)).toBe(new Date(v).toLocaleString(undefined, { month: 'short', day: 'numeric' }))
  })
  it('shows the year on the two and three year periods', () => {
    for (const p of ['biennially', 'triennially']) {
      expect(axisTimeFormat(p)(v)).toBe(new Date(v).toLocaleString(undefined, { month: 'short', year: 'numeric' }))
      expect(axisTimeFormat(p)(v)).toContain('2025')
    }
  })
})

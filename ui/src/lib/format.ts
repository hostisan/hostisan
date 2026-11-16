const usageMeasures = ['mb', 'gb', 'tb', 'pb'] as const

/**
 * Hestia's humanize_usage_size and humanize_usage_measure: megabytes stepped up while
 * above 1024, then once more when the result is above 999 (so 1000 MB reads 0.98 gb).
 * "unlimited" is ∞ with no measure. The size has a thousands separator.
 */
export function humanizeUsage(mb: string, round = 2): { size: string; measure: string } {
  if (mb === 'unlimited') return { size: '∞', measure: '' }
  let value = Number(mb)
  if (!Number.isFinite(value) || value < 1) value = 0
  let i = 0
  while (value > 1024 && i < usageMeasures.length - 1) {
    value /= 1024
    i++
  }
  if (value > 999 && i < usageMeasures.length - 1) {
    value /= 1024
    i++
  }
  const size = value.toLocaleString('en-US', { minimumFractionDigits: round, maximumFractionDigits: round })
  return { size, measure: usageMeasures[i] }
}

/** Hestia reports sizes in megabytes as strings ("1840", "unlimited"): "1.80 GB", or ∞. */
export function formatMB(value: string, round = 2): string {
  const { size, measure } = humanizeUsage(value, round)
  return measure ? `${size} ${measure.toUpperCase()}` : size
}

export function usagePercent(used: string, quota: string): number | null {
  if (quota === 'unlimited') return null
  const u = Number(used)
  const q = Number(quota)
  if (!q) return null
  return Math.min(100, Math.round((u / q) * 100))
}

// "PHP-8_3" -> "PHP 8.3"
export function formatBackend(tpl: string): string {
  const match = /^PHP-(\d+)_(\d+)$/.exec(tpl)
  return match ? `PHP ${match[1]}.${match[2]}` : tpl
}

import { TagInput } from '@cloudflare/kumo'
import { useState } from 'react'
import { useT } from '../../i18n/I18nProvider'
import { isHostname } from '../../lib/validate'
import { useFormGuard } from '../../unsaved/types'

const MIN = 2
const MAX = 8

/**
 * Name servers as tags, for DNS zones, users and packages. The panel requires two and
 * allows up to eight; each is submitted as v_ns1…v_ns8. A trailing dot is accepted.
 */
export function NameServersField({
  defaultValue,
  disabled,
  onChange,
}: {
  defaultValue: string | string[]
  disabled?: boolean
  onChange?: (servers: string[]) => void
}) {
  const t = useT()
  const guard = useFormGuard()
  const [servers, setServers] = useState(() =>
    Array.isArray(defaultValue) ? defaultValue : defaultValue.split(',').filter(Boolean),
  )
  const tooFew = servers.length < MIN && !disabled
  return (
    <>
      <TagInput
        label={t('Name servers')}
        description={t('Between %d and %d host names. Press Enter or comma after each one.', MIN, MAX)}
        value={servers}
        onValueChange={(v) => {
          guard?.markDirty()
          setServers(v)
          onChange?.(v)
        }}
        maxValues={MAX}
        placeholder="ns1.acme-hosting.net"
        autoComplete="off"
        disabled={disabled}
        validateValue={(v, accepted) => isHostname(v.replace(/\.$/, '')) && !accepted.includes(v)}
        labels={{
          invalidValue: (v) => t("“%s” isn't a valid host name, or it's already in the list.", v),
          maxValuesReached: (n) => t('Up to %d name servers are allowed.', n),
        }}
        error={tooFew ? t('Add at least %d name servers.', MIN) : undefined}
        variant={tooFew ? 'error' : 'default'}
      />
      {servers.map((ns, i) => (
        <input key={ns} type="hidden" name={`v_ns${i + 1}`} value={ns} />
      ))}
    </>
  )
}

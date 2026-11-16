import { Input } from '@cloudflare/kumo'
import { MagnifyingGlassIcon } from '@phosphor-icons/react'
import { useT } from '../i18n/I18nProvider'

type SearchInputProps = {
  label: string
  placeholder?: string
  value: string
  onChange: (value: string) => void
}

export function SearchInput({ label, placeholder, value, onChange }: SearchInputProps) {
  const t = useT()
  return (
    <div className="relative w-full max-w-xs">
      <MagnifyingGlassIcon className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-kumo-subtle" />
      <Input
        aria-label={label}
        placeholder={placeholder ?? t('Search')}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="pl-8"
      />
    </div>
  )
}

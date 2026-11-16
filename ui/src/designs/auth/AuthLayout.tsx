import { LayerCard, Link, Select, Text } from '@cloudflare/kumo'
import { GlobeIcon } from '@phosphor-icons/react'
import { useState, type ReactNode } from 'react'
import { useT } from '../../i18n/I18nProvider'
import { nativeLanguageName } from '../../lib/language'
import { useSession } from '../../session/SessionProvider'

type AuthLayoutProps = {
  title: string
  description?: ReactNode
  children: ReactNode
}

function BrandMark() {
  const appName = useSession().bootstrap?.appName ?? 'Hostisan'
  return (
    <Link href="/login" variant="plain" className="flex items-center gap-2 text-kumo-default">
      <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-kumo-brand text-base font-semibold text-white">
        H
      </span>
      <Text variant="heading" size="lg" as="span">
        {appName}
      </Text>
    </Link>
  )
}

function LanguageSelect() {
  const t = useT()
  const { bootstrap, setLanguage: applyLanguage } = useSession()
  const [language, setLanguage] = useState(bootstrap?.language ?? 'en')
  // Codes come from the server; each is named in its own language.
  const items = Object.fromEntries((bootstrap?.languages ?? ['en']).map((code) => [code, nativeLanguageName(code)]))
  return (
    <div className="flex items-center gap-1.5">
      <GlobeIcon className="text-kumo-subtle" aria-hidden />
      <Select
        aria-label={t('Language')}
        size="sm"
        className="w-44"
        value={language}
        onValueChange={(v) => {
          const lang = (v as string | null) ?? 'en'
          setLanguage(lang)
          applyLanguage(lang)
        }}
        items={items}
      />
    </div>
  )
}

/** Shell for the routes that sit outside the app layout: login and the reset flows. */
export function AuthLayout({ title, description, children }: AuthLayoutProps) {
  const { appName = 'Hostisan', version = '' } = useSession().bootstrap ?? {}
  return (
    <div className="flex min-h-full flex-col bg-kumo-canvas">
      <main className="flex flex-1 flex-col items-center justify-center gap-8 px-4 py-10">
        <BrandMark />
        <LayerCard className="w-full max-w-sm px-6 py-5">
          <div className="grid gap-5">
            <div className="grid gap-1">
              <Text variant="heading" size="lg" as="h1">
                {title}
              </Text>
              {description && <Text variant="secondary">{description}</Text>}
            </div>
            {children}
          </div>
        </LayerCard>
      </main>
      <footer className="flex flex-wrap items-center justify-between gap-3 px-4 py-4 sm:px-6">
        <Text variant="secondary" size="sm">
          {appName} {version}
        </Text>
        <LanguageSelect />
      </footer>
    </div>
  )
}

/** "Signed in as acme · change" line shown on the later login steps. */
export function UsernameChip({ username, onChange }: { username: string; onChange: () => void }) {
  const t = useT()
  return (
    <div className="flex items-center justify-between gap-3 rounded-lg bg-kumo-tint px-3 py-2">
      <Text as="span">
        <Text as="span" variant="secondary">
          {t('Username')}{' '}
        </Text>
        <Text as="span" bold>
          {username}
        </Text>
      </Text>
      <button type="button" onClick={onChange} className="text-sm text-kumo-link hover:underline">
        {t('Change')}
      </button>
    </div>
  )
}

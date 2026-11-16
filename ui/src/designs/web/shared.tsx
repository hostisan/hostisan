import { Badge, Empty, LinkButton } from '@cloudflare/kumo'
import { GlobeIcon } from '@phosphor-icons/react'
import { useParams } from 'react-router'
import type { WebDomain } from '../../api/hestia.gen'
import { useWebDomain } from '../../api/web'
import { useT } from '../../i18n/I18nProvider'

/** The :domain route parameter and its GET /web/{domain} query (idle without one). */
export function useWebDomainParam() {
  const { domain: name = '' } = useParams()
  return { name, query: useWebDomain(name) }
}

export function DomainNotFound({ name }: { name: string }) {
  const t = useT()
  return (
    <Empty
      icon={<GlobeIcon size={40} className="text-kumo-inactive" />}
      title={t('Web domain not found')}
      description={t('There is no web domain named %s on this account.', name)}
      contents={<LinkButton href="/web">{t('Back to web domains')}</LinkButton>}
      className="py-16"
    />
  )
}

/** The SSL state of a list row or a domain detail: none, Let's Encrypt or custom. */
export function SslBadge({ domain }: { domain: Pick<WebDomain, 'SSL' | 'LETSENCRYPT'> }) {
  const t = useT()
  if (domain.SSL !== 'yes') return <Badge variant="neutral">{t('No SSL')}</Badge>
  if (domain.LETSENCRYPT === 'yes') return <Badge variant="green">Let's Encrypt</Badge>
  return <Badge variant="blue">{t('Custom')}</Badge>
}

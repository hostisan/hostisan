import { Text, useLinkComponent } from '@cloudflare/kumo'
import { Fragment } from 'react'
import { useT } from '../i18n/I18nProvider'
import { msgid } from '../i18n/msgid'
import { useSession, useSessionUser } from '../session/SessionProvider'
import { docsUrl, helpLink } from './help'

type FooterLink = { label: string; href: string; external?: boolean; server?: boolean }

/**
 * The footer links after Documentation. The security address and the site come from
 * the bootstrap (install/common/hostisan/product.env, built into the binary); a build
 * without them leaves their links out.
 */
function footerLinks(productUrl: string, securityEmail: string): FooterLink[] {
  return [
    { label: msgid('Server status'), href: '/server/services', server: true },
    ...(securityEmail ? [{ label: msgid('Report a security issue'), href: `mailto:${securityEmail}`, external: true }] : []),
    ...(productUrl
      ? [
          { label: msgid('Terms of use'), href: `${productUrl}/terms`, external: true },
          { label: msgid('Privacy policy'), href: `${productUrl}/privacy`, external: true },
        ]
      : []),
  ]
}

/**
 * Last item of the scrolling content pane. From lg it matches Kumo's Sidebar.Footer
 * (h-12, top border), so on short pages the two line up; narrower, the links wrap.
 */
export function AppFooter() {
  // Kumo's styled Link is blue and underlined; the dashboard footer uses plain text links.
  const RouterLink = useLinkComponent()
  const { bootstrap } = useSession()
  const { appName = 'Hostisan', version = '', productUrl = '', securityEmail = '' } = bootstrap ?? {}
  const t = useT()
  // Server status is the server section: only for users who may open it.
  const canSeeServer = useSessionUser()?.features.server ?? false
  // Documentation follows the top bar's Help item: hidden by HIDE_DOCS or without docs (D17).
  const docs = helpLink(bootstrap?.hideDocs ?? false, docsUrl)
  const shown = [
    ...(docs ? [{ label: msgid('Documentation'), href: docs, external: true }] : []),
    ...footerLinks(productUrl, securityEmail).filter((link) => !link.server || canSeeServer),
  ]
  return (
    <footer className="flex shrink-0 flex-wrap items-center justify-center gap-x-4 gap-y-2 border-t border-kumo-line bg-kumo-base px-4 py-3 whitespace-nowrap lg:h-12 lg:py-0">
      <nav aria-label={t('Footer')} className="flex flex-wrap items-center justify-center gap-x-4 gap-y-1 lg:gap-x-0">
        {shown.map((link, i) => (
          <Fragment key={link.label}>
            {i > 0 && <span aria-hidden className="mx-4 hidden h-3.5 w-px bg-kumo-line lg:block" />}
            <RouterLink
              href={link.href}
              target={link.external ? '_blank' : undefined}
              rel={link.external ? 'noreferrer' : undefined}
              className="text-kumo-default hover:underline"
            >
              {t(link.label)}
            </RouterLink>
          </Fragment>
        ))}
      </nav>
      <Text variant="secondary" as="span">
        © {new Date().getFullYear()} {appName}{version && ` · v${version.replace(/^v/, '')}`}
      </Text>
    </footer>
  )
}

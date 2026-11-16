import type { RouteObject } from 'react-router'
import { MailAccountFormPage } from './MailAccountFormPage'
import { MailAccountsPage } from './MailAccountsPage'
import { MailDnsPage } from './MailDnsPage'
import { MailDomainFormPage } from './MailDomainFormPage'
import { MailDomainsPage } from './MailDomainsPage'

export const routes: RouteObject[] = [
  { path: '/mail', element: <MailDomainsPage /> },
  { path: '/mail/new', element: <MailDomainFormPage mode="add" /> },
  { path: '/mail/:domain', element: <MailAccountsPage /> },
  { path: '/mail/:domain/edit', element: <MailDomainFormPage mode="edit" /> },
  { path: '/mail/:domain/dns', element: <MailDnsPage /> },
  { path: '/mail/:domain/accounts/new', element: <MailAccountFormPage mode="add" /> },
  { path: '/mail/:domain/accounts/:account', element: <MailAccountFormPage mode="edit" /> },
]

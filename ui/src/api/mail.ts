import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'
import type {
  AddMailAccountRequest,
  AddMailDomainRequest,
  BulkReply,
  BulkRequest,
  EditMailAccountRequest,
  EditMailDomainRequest,
  MailAccounts,
  MailAccountView,
  MailDNSView,
  MailDomains,
  MailDomainView,
  MailOptions,
  MailSSLRequest,
  OK,
} from './types.gen'

const domainPath = (domain: string) => `/mail/${encodeURIComponent(domain)}`
const accountsPath = (domain: string) => `${domainPath(domain)}/accounts`
const accountPath = (domain: string, account: string) => `${accountsPath(domain)}/${encodeURIComponent(account)}`

/** Every mail query key starts with 'mail', so invalidating ['mail'] refetches them all. */
export const mailKeys = {
  all: ['mail'] as const,
  list: ['mail', 'list'] as const,
  options: ['mail', 'options'] as const,
  domain: (domain: string) => ['mail', 'domain', domain] as const,
  dns: (domain: string) => ['mail', 'domain', domain, 'dns'] as const,
  accounts: (domain: string) => ['mail', 'domain', domain, 'accounts'] as const,
  account: (domain: string, account: string) => ['mail', 'domain', domain, 'accounts', account] as const,
}

/** GET /mail: the viewed account's mail domains and usage. */
export function useMailDomains({ enabled = true } = {}) {
  return useQuery({ queryKey: mailKeys.list, queryFn: () => api.get<MailDomains>('/mail'), enabled })
}

/** GET /mail/options: what the server offers to the mail forms and lists. */
export function useMailOptions() {
  return useQuery({
    queryKey: mailKeys.options,
    queryFn: () => api.get<MailOptions>('/mail/options'),
    staleTime: 60_000,
  })
}

/** GET /mail/{domain}: one domain (and its certificate); idle while domain is empty. */
export function useMailDomain(domain: string) {
  return useQuery({
    queryKey: mailKeys.domain(domain),
    queryFn: () => api.get<MailDomainView>(domainPath(domain)),
    enabled: domain !== '',
  })
}

/** GET /mail/{domain}/dns: the DNS records to publish; idle while domain is empty. */
export function useMailDns(domain: string) {
  return useQuery({
    queryKey: mailKeys.dns(domain),
    queryFn: () => api.get<MailDNSView>(`${domainPath(domain)}/dns`),
    enabled: domain !== '',
  })
}

/** GET /mail/{domain}/accounts; idle while domain is empty. */
export function useMailAccounts(domain: string) {
  return useQuery({
    queryKey: mailKeys.accounts(domain),
    queryFn: () => api.get<MailAccounts>(accountsPath(domain)),
    enabled: domain !== '',
  })
}

/** GET /mail/{domain}/accounts/{account}; idle while either is empty (the add page). */
export function useMailAccount(domain: string, account: string) {
  return useQuery({
    queryKey: mailKeys.account(domain, account),
    queryFn: () => api.get<MailAccountView>(accountPath(domain, account)),
    enabled: domain !== '' && account !== '',
  })
}

/**
 * Every write to mail. Each call refetches all mail queries when it settles (success
 * or failure), and rejects with an ApiError whose message is Hestia's text.
 */
export function useMailMutation() {
  const client = useQueryClient()
  const run = useMutation({
    mutationFn: (call: () => Promise<unknown>) => call(),
    onSettled: () => client.invalidateQueries({ queryKey: mailKeys.all }),
  })
  const go = <T>(call: () => Promise<T>) => run.mutateAsync(call) as Promise<T>
  return {
    addDomain: (body: AddMailDomainRequest) => go(() => api.post<OK>('/mail', body)),
    editDomain: (domain: string, body: EditMailDomainRequest) => go(() => api.put<OK>(domainPath(domain), body)),
    saveSsl: (domain: string, body: MailSSLRequest) => go(() => api.put<OK>(`${domainPath(domain)}/ssl`, body)),
    deleteDomain: (domain: string) => go(() => api.del<OK>(domainPath(domain))),
    suspendDomain: (domain: string) => go(() => api.post<OK>(`${domainPath(domain)}/suspend`)),
    unsuspendDomain: (domain: string) => go(() => api.post<OK>(`${domainPath(domain)}/unsuspend`)),
    bulkDomains: (body: BulkRequest) => go(() => api.post<BulkReply>('/mail/bulk', body)),
    addAccount: (domain: string, body: AddMailAccountRequest) => go(() => api.post<OK>(accountsPath(domain), body)),
    editAccount: (domain: string, account: string, body: EditMailAccountRequest) =>
      go(() => api.put<OK>(accountPath(domain, account), body)),
    deleteAccount: (domain: string, account: string) => go(() => api.del<OK>(accountPath(domain, account))),
    suspendAccount: (domain: string, account: string) =>
      go(() => api.post<OK>(`${accountPath(domain, account)}/suspend`)),
    unsuspendAccount: (domain: string, account: string) =>
      go(() => api.post<OK>(`${accountPath(domain, account)}/unsuspend`)),
    bulkAccounts: (domain: string, body: BulkRequest) =>
      go(() => api.post<BulkReply>(`${accountsPath(domain)}/bulk`, body)),
  }
}

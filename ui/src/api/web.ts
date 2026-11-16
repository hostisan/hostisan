import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'
import { useJobStatus } from './jobs'
import type {
  BulkReply,
  BulkRequest,
  JobRef,
  OK,
  WebAddRequest,
  WebAppForm,
  WebAppInstallRequest,
  WebApps,
  WebDomains,
  WebDomainView,
  WebEditRequest,
  WebGeneratedSSL,
  WebGenerateSSLRequest,
  WebLog,
  WebOptions,
  WebSSLRequest,
} from './types.gen'

/** Which web log: Hestia's access or error log. */
export type WebLogType = WebLog['type']

const enc = encodeURIComponent
const domainPath = (domain: string) => `/web/${enc(domain)}`
const appsPath = (domain: string) => `${domainPath(domain)}/apps`

/** Every web query key starts with 'web', so invalidating ['web'] refetches them all. */
export const webKeys = {
  all: ['web'] as const,
  list: ['web', 'list'] as const,
  options: ['web', 'options'] as const,
  domain: (domain: string) => ['web', 'domain', domain] as const,
  log: (domain: string, type: WebLogType) => ['web', 'domain', domain, 'log', type] as const,
  apps: (domain: string) => ['web', 'domain', domain, 'apps'] as const,
  app: (domain: string, app: string) => ['web', 'domain', domain, 'apps', app] as const,
}

/** GET /web: the viewed account's web domains, their display IPs and usage. */
export function useWebDomains({ enabled = true } = {}) {
  return useQuery({ queryKey: webKeys.list, queryFn: () => api.get<WebDomains>('/web'), enabled })
}

/** GET /web/options: what the server offers to the web forms and lists. */
export function useWebOptions() {
  return useQuery({
    queryKey: webKeys.options,
    queryFn: () => api.get<WebOptions>('/web/options'),
    staleTime: 60_000,
  })
}

/** GET /web/{domain}: the edit form's state; idle while domain is empty. */
export function useWebDomain(domain: string) {
  return useQuery({
    queryKey: webKeys.domain(domain),
    queryFn: () => api.get<WebDomainView>(domainPath(domain)),
    enabled: domain !== '',
  })
}

/** GET /web/{domain}/logs?type=: the last 70 lines; idle while domain is empty. */
export function useWebLog(domain: string, type: WebLogType) {
  return useQuery({
    queryKey: webKeys.log(domain, type),
    queryFn: () => api.get<WebLog>(`${domainPath(domain)}/logs?type=${type}`),
    enabled: domain !== '',
  })
}

/** GET /web/{domain}/apps: the quick-install catalogue; idle while domain is empty. */
export function useWebApps(domain: string) {
  return useQuery({
    queryKey: webKeys.apps(domain),
    queryFn: () => api.get<WebApps>(appsPath(domain)),
    enabled: domain !== '',
  })
}

/** GET /web/{domain}/apps/{app}: an installer's setup form; idle while either is empty. */
export function useWebApp(domain: string, app: string) {
  return useQuery({
    queryKey: webKeys.app(domain, app),
    queryFn: () => api.get<WebAppForm>(`${appsPath(domain)}/${enc(app)}`),
    enabled: domain !== '' && app !== '',
  })
}

/**
 * GET /web/{domain}/apps/jobs/{job}: an install job's JobStatus, polled every 2 s while
 * it is running. No request is made while domain or job is empty. A 404 means the job
 * is unknown (expired, or lost on a restart).
 */
export function useWebAppJob(domain: string, job: string) {
  return useJobStatus(domain !== '' && job !== '' ? `${appsPath(domain)}/jobs/${enc(job)}` : undefined, 2000)
}

/** The access or error log download: the last 5000 lines as a text attachment. */
export const webLogDownloadUrl = (domain: string, type: WebLogType) =>
  `/api/v1${domainPath(domain)}/logs/download?type=${type}`

/** The whole site as a zip. */
export const webSiteDownloadUrl = (domain: string) => `/api/v1${domainPath(domain)}/download`

/**
 * Every write to web. Each call refetches all web queries when it settles (success or
 * failure), and rejects with an ApiError whose message is Hestia's text. After a
 * delete succeeds, the deleted domain's own queries are left out: its edit page, still
 * mounted until it moves to /web, would only get 404s back.
 */
export function useWebMutation() {
  const client = useQueryClient()
  const run = useMutation({
    mutationFn: ({ call }: { call: () => Promise<unknown>; deletes?: string }) => call(),
    onSettled: (_data, error, { deletes }) => {
      if (error || !deletes) return client.invalidateQueries({ queryKey: webKeys.all })
      const gone = webKeys.domain(deletes)
      const isGone = (key: readonly unknown[]) => gone.every((part, i) => key[i] === part)
      return client.invalidateQueries({ queryKey: webKeys.all, predicate: (q) => !isGone(q.queryKey) })
    },
  })
  const go = <T>(call: () => Promise<T>, deletes?: string) => run.mutateAsync({ call, deletes }) as Promise<T>
  return {
    addDomain: (body: WebAddRequest) => go(() => api.post<OK>('/web', body)),
    editDomain: (domain: string, body: WebEditRequest) => go(() => api.put<OK>(domainPath(domain), body)),
    saveSsl: (domain: string, body: WebSSLRequest) => go(() => api.put<OK>(`${domainPath(domain)}/ssl`, body)),
    deleteDomain: (domain: string) => go(() => api.del<OK>(domainPath(domain)), domain),
    suspendDomain: (domain: string) => go(() => api.post<OK>(`${domainPath(domain)}/suspend`)),
    unsuspendDomain: (domain: string) => go(() => api.post<OK>(`${domainPath(domain)}/unsuspend`)),
    purgeCache: (domain: string) => go(() => api.post<OK>(`${domainPath(domain)}/purge-cache`)),
    bulkDomains: (body: BulkRequest) => go(() => api.post<BulkReply>('/web/bulk', body)),
    generateSsl: (body: WebGenerateSSLRequest) => go(() => api.post<WebGeneratedSSL>('/web/generate-ssl', body)),
    installApp: (domain: string, app: string, body: WebAppInstallRequest) =>
      go(() => api.post<JobRef>(`${appsPath(domain)}/${enc(app)}`, body)),
  }
}

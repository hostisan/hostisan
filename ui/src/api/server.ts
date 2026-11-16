import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'
import { useJobStatus } from './jobs'
import type {
  AutoupdateRequest,
  BanDeleteRequest,
  BanRequest,
  BansView,
  BulkReply,
  BulkRequest,
  EditorSaveRequest,
  EditorView,
  FirewallRuleRequest,
  FirewallRuleRow,
  FirewallToggleRequest,
  FirewallView,
  GraphCatalog,
  GraphData,
  IPOptions,
  IPRequest,
  IPRow,
  IPsView,
  IpsetRequest,
  IpsetsView,
  JobRef,
  OK,
  PreviewFeatures,
  RebootRequest,
  RuleActionRequest,
  RuleBulkRequest,
  ServiceActionRequest,
  ServicesView,
  SettingsRequest,
  SettingsView,
  StatusText,
  UpdatesView,
  WhiteLabelRequest,
  WhiteLabelView,
} from './types.gen'

// Server admin (Plan 4) data layer. Every path is under /api/v1/server; writes that
// run long work resolve to a JobRef, polled with useJob (or <JobProgress>).

const enc = encodeURIComponent

/** Every server query key starts with 'server', so invalidating ['server'] refetches them all. */
export const serverKeys = {
  all: ['server'] as const,
  services: ['server', 'services'] as const,
  editor: (service: string) => ['server', 'editor', service] as const,
  settings: ['server', 'settings'] as const,
  status: (kind: string) => ['server', 'status', kind] as const,
  graphs: ['server', 'graphs'] as const,
  graph: (id: string, period: string) => ['server', 'graphs', id, period] as const,
  updates: ['server', 'updates'] as const,
  whiteLabel: ['server', 'white-label'] as const,
  preview: ['server', 'preview'] as const,
  ips: ['server', 'ips'] as const,
  ipOptions: ['server', 'ips', 'options'] as const,
  ip: (ip: string) => ['server', 'ips', 'one', ip] as const,
  firewall: ['server', 'firewall'] as const,
  rule: (id: string) => ['server', 'firewall', 'rule', id] as const,
  bans: ['server', 'firewall', 'bans'] as const,
  ipsets: ['server', 'firewall', 'ipsets'] as const,
}

/** GET /server/services: system summary and the services, sorted by name. */
export function useServices() {
  return useQuery({ queryKey: serverKeys.services, queryFn: () => api.get<ServicesView>('/server/services') })
}

/** GET /server/editors/{service}: one service's config editor; idle while service is empty. */
export function useEditor(service: string) {
  return useQuery({
    queryKey: serverKeys.editor(service),
    queryFn: () => api.get<EditorView>(`/server/editors/${enc(service)}`),
    enabled: service !== '',
  })
}

/** GET /server/settings: the Configure Server form. */
export function useSettings() {
  return useQuery({ queryKey: serverKeys.settings, queryFn: () => api.get<SettingsView>('/server/settings') })
}

/** GET /server/status/{kind} (cpu mem disk net web dns mail db); idle while kind is empty. */
export function useStatus(kind: string) {
  return useQuery({
    queryKey: serverKeys.status(kind),
    queryFn: () => api.get<StatusText>(`/server/status/${enc(kind)}`),
    enabled: kind !== '',
  })
}

/** GET /server/graphs: the chart catalogue. */
export function useGraphs() {
  return useQuery({ queryKey: serverKeys.graphs, queryFn: () => api.get<GraphCatalog>('/server/graphs') })
}

/** GET /server/graphs/{id}?period=: one chart's series; idle while either is empty. */
export function useGraph(id: string, period: string) {
  return useQuery({
    queryKey: serverKeys.graph(id, period),
    queryFn: () => api.get<GraphData>(`/server/graphs/${enc(id)}?period=${enc(period)}`),
    enabled: id !== '' && period !== '',
  })
}

/** GET /server/updates: the core packages and the automatic-update switch. */
export function useUpdates() {
  return useQuery({ queryKey: serverKeys.updates, queryFn: () => api.get<UpdatesView>('/server/updates') })
}

/** GET /server/white-label. */
export function useWhiteLabel() {
  return useQuery({ queryKey: serverKeys.whiteLabel, queryFn: () => api.get<WhiteLabelView>('/server/white-label') })
}

/** GET /server/preview: the preview features list. */
export function usePreview() {
  return useQuery({ queryKey: serverKeys.preview, queryFn: () => api.get<PreviewFeatures>('/server/preview') })
}

/** GET /server/ips. */
export function useIps() {
  return useQuery({ queryKey: serverKeys.ips, queryFn: () => api.get<IPsView>('/server/ips') })
}

/** GET /server/ips/options: interfaces, users and the root user for the IP forms. */
export function useIpOptions() {
  return useQuery({ queryKey: serverKeys.ipOptions, queryFn: () => api.get<IPOptions>('/server/ips/options') })
}

/** GET /server/ips/{ip}; idle while ip is empty (the add page). */
export function useIp(ip: string) {
  return useQuery({
    queryKey: serverKeys.ip(ip),
    queryFn: () => api.get<IPRow>(`/server/ips/${enc(ip)}`),
    enabled: ip !== '',
  })
}

/** GET /server/firewall: the rules in evaluation order. */
export function useFirewall() {
  return useQuery({ queryKey: serverKeys.firewall, queryFn: () => api.get<FirewallView>('/server/firewall') })
}

/** GET /server/firewall/rules/{id}; idle while id is empty (the add page). */
export function useFirewallRule(id: string) {
  return useQuery({
    queryKey: serverKeys.rule(id),
    queryFn: () => api.get<FirewallRuleRow>(`/server/firewall/rules/${enc(id)}`),
    enabled: id !== '',
  })
}

/** GET /server/firewall/bans. */
export function useBans() {
  return useQuery({ queryKey: serverKeys.bans, queryFn: () => api.get<BansView>('/server/firewall/bans') })
}

/** GET /server/firewall/ipsets. */
export function useIpsets() {
  return useQuery({ queryKey: serverKeys.ipsets, queryFn: () => api.get<IpsetsView>('/server/firewall/ipsets') })
}

/** Polls a server job every second until it is done or failed; idle without an id. */
export function useJob(id: string) {
  return useJobStatus(id ? `/server/jobs/${enc(id)}` : undefined, 1000)
}

/**
 * Every write to the server section. Each call refetches all server queries when it
 * settles (success or failure) and rejects with an ApiError. Job routes resolve to
 * their JobRef; follow it with useJob or <JobProgress>.
 *
 * One useMutation is shared by every call, so its isPending/variables describe only the
 * latest call: screens track per-call busy state themselves (e.g. which row is acting).
 */
export function useServerMutation() {
  const client = useQueryClient()
  const run = useMutation({
    mutationFn: (call: () => Promise<unknown>) => call(),
    onSettled: () => client.invalidateQueries({ queryKey: serverKeys.all }),
  })
  const go = <T>(call: () => Promise<T>) => run.mutateAsync(call) as Promise<T>
  return {
    // Services and editors (jobs).
    serviceAction: (service: string, action: 'start' | 'stop' | 'restart', body: ServiceActionRequest = {}) =>
      go(() => api.post<JobRef>(`/server/services/${enc(service)}/${action}`, body)),
    bulkServices: (body: BulkRequest) => go(() => api.post<JobRef>('/server/services/bulk', body)),
    reboot: (body: RebootRequest) => go(() => api.post<JobRef>('/server/reboot', body)),
    saveEditor: (service: string, body: EditorSaveRequest) =>
      go(() => api.put<JobRef>(`/server/editors/${enc(service)}`, body)),
    // Configure Server (job).
    saveSettings: (body: SettingsRequest) => go(() => api.put<JobRef>('/server/settings', body)),
    // Updates and white label.
    setAutoupdate: (body: AutoupdateRequest) => go(() => api.post<OK>('/server/updates/autoupdate', body)),
    saveWhiteLabel: (body: WhiteLabelRequest) => go(() => api.put<OK>('/server/white-label', body)),
    // IP addresses.
    addIp: (body: IPRequest) => go(() => api.post<JobRef>('/server/ips', body)),
    editIp: (ip: string, body: IPRequest) => go(() => api.put<OK>(`/server/ips/${enc(ip)}`, body)),
    // deleteIp and refreshIps send no body; their handlers must not call decodeBody.
    deleteIp: (ip: string) => go(() => api.del<JobRef>(`/server/ips/${enc(ip)}`)),
    bulkIps: (body: BulkRequest) => go(() => api.post<JobRef>('/server/ips/bulk', body)),
    refreshIps: () => go(() => api.post<JobRef>('/server/ips/refresh')),
    // Firewall rules.
    addRule: (body: FirewallRuleRequest) => go(() => api.post<OK>('/server/firewall/rules', body)),
    editRule: (id: string, body: FirewallRuleRequest) =>
      go(() => api.put<OK>(`/server/firewall/rules/${enc(id)}`, body)),
    ruleAction: (
      id: string,
      action: 'delete' | 'suspend' | 'unsuspend' | 'move-up' | 'move-down',
      body: RuleActionRequest,
    ) => go(() => api.post<OK>(`/server/firewall/rules/${enc(id)}/${action}`, body)),
    bulkRules: (body: RuleBulkRequest) => go(() => api.post<BulkReply>('/server/firewall/rules/bulk', body)),
    toggleFirewall: (body: FirewallToggleRequest) => go(() => api.post<JobRef>('/server/firewall/toggle', body)),
    // Bans and IP sets.
    addBan: (body: BanRequest) => go(() => api.post<OK>('/server/firewall/bans', body)),
    deleteBans: (body: BanDeleteRequest) => go(() => api.post<BulkReply>('/server/firewall/bans/delete', body)),
    addIpset: (body: IpsetRequest) => go(() => api.post<JobRef>('/server/firewall/ipsets', body)),
    bulkIpsets: (body: BulkRequest) => go(() => api.post<BulkReply>('/server/firewall/ipsets/bulk', body)),
    deleteIpset: (name: string) => go(() => api.del<OK>(`/server/firewall/ipsets/${enc(name)}`)),
  }
}

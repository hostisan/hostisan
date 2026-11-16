import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from './client'
import type { DNSDomain } from './hestia.gen'
import type {
  AddZoneRequest,
  BulkReply,
  BulkRequest,
  DNSOptions,
  DNSRecords,
  DNSSECView,
  DNSZones,
  EditZoneRequest,
  OK,
  RecordRequest,
} from './types.gen'

const zonePath = (domain: string) => `/dns/${encodeURIComponent(domain)}`
const recordPath = (domain: string, id: string) => `${zonePath(domain)}/records/${encodeURIComponent(id)}`

export const dnsKeys = {
  all: ['dns'] as const,
  zone: (domain: string) => ['dns', domain] as const,
  records: (domain: string) => ['dns', domain, 'records'] as const,
  dnssec: (domain: string) => ['dns', domain, 'dnssec'] as const,
  options: ['dns', 'options'] as const,
}

export function useDnsZones({ enabled = true } = {}) {
  return useQuery({
    queryKey: dnsKeys.all,
    queryFn: () => api.get<DNSZones>('/dns'),
    enabled,
  })
}

/** One zone; idle while domain is empty (the add-zone page has none). */
export function useDnsZone(domain: string) {
  return useQuery({
    queryKey: dnsKeys.zone(domain),
    queryFn: () => api.get<DNSDomain>(zonePath(domain)),
    enabled: domain !== '',
  })
}

export function useDnsRecords(domain: string) {
  return useQuery({
    queryKey: dnsKeys.records(domain),
    queryFn: () => api.get<DNSRecords>(`${zonePath(domain)}/records`),
  })
}

export function useDnssec(domain: string) {
  return useQuery({
    queryKey: dnsKeys.dnssec(domain),
    queryFn: () => api.get<DNSSECView>(`${zonePath(domain)}/dnssec`),
  })
}

export function useDnsOptions() {
  return useQuery({ queryKey: dnsKeys.options, queryFn: () => api.get<DNSOptions>('/dns/options'), staleTime: 60_000 })
}

/**
 * Every write to DNS. Each call refetches the DNS queries when it settles, and rejects
 * with an ApiError whose message is Hestia's text.
 */
export function useDnsMutation() {
  const client = useQueryClient()
  const run = useMutation({
    mutationFn: (call: () => Promise<unknown>) => call(),
    onSettled: () => client.invalidateQueries({ queryKey: dnsKeys.all }),
  })
  const go = <T>(call: () => Promise<T>) => run.mutateAsync(call) as Promise<T>
  return {
    addZone: (body: AddZoneRequest) => go(() => api.post<OK>('/dns', body)),
    editZone: (domain: string, body: EditZoneRequest) => go(() => api.put<OK>(zonePath(domain), body)),
    deleteZone: (domain: string) => go(() => api.del<OK>(zonePath(domain))),
    suspendZone: (domain: string) => go(() => api.post<OK>(`${zonePath(domain)}/suspend`)),
    unsuspendZone: (domain: string) => go(() => api.post<OK>(`${zonePath(domain)}/unsuspend`)),
    bulkZones: (body: BulkRequest) => go(() => api.post<BulkReply>('/dns/bulk', body)),
    addRecord: (domain: string, body: RecordRequest) => go(() => api.post<OK>(`${zonePath(domain)}/records`, body)),
    editRecord: (domain: string, id: string, body: RecordRequest) =>
      go(() => api.put<OK>(recordPath(domain, id), body)),
    deleteRecord: (domain: string, id: string) => go(() => api.del<OK>(recordPath(domain, id))),
    suspendRecord: (domain: string, id: string) => go(() => api.post<OK>(`${recordPath(domain, id)}/suspend`)),
    unsuspendRecord: (domain: string, id: string) => go(() => api.post<OK>(`${recordPath(domain, id)}/unsuspend`)),
    bulkRecords: (domain: string, body: BulkRequest) =>
      go(() => api.post<BulkReply>(`${zonePath(domain)}/records/bulk`, body)),
  }
}

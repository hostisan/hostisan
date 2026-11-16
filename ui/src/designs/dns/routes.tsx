import type { RouteObject } from 'react-router'
import { DnsRecordFormPage } from './DnsRecordFormPage'
import { DnsRecordsPage } from './DnsRecordsPage'
import { DnssecPage } from './DnssecPage'
import { DnsZoneFormPage } from './DnsZoneFormPage'
import { DnsZonesPage } from './DnsZonesPage'

export const routes: RouteObject[] = [
  { path: '/dns', element: <DnsZonesPage /> },
  { path: '/dns/new', element: <DnsZoneFormPage mode="add" /> },
  { path: '/dns/:domain', element: <DnsRecordsPage /> },
  { path: '/dns/:domain/edit', element: <DnsZoneFormPage mode="edit" /> },
  { path: '/dns/:domain/dnssec', element: <DnssecPage /> },
  { path: '/dns/:domain/records/new', element: <DnsRecordFormPage mode="add" /> },
  { path: '/dns/:domain/records/:id', element: <DnsRecordFormPage mode="edit" /> },
]

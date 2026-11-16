import type { RouteObject } from 'react-router'
import { BackupDetailPage } from './BackupDetailPage'
import { BackupsPage } from './BackupsPage'
import { BackupExclusionsPage, EditBackupExclusionsPage } from './ExclusionsPages'
import { IncrementalBackupsPage, SnapshotDetailPage, SnapshotFilesPage } from './IncrementalPages'

export const routes: RouteObject[] = [
  { path: '/backup', element: <BackupsPage /> },
  { path: '/backup/exclusions', element: <BackupExclusionsPage /> },
  { path: '/backup/exclusions/edit', element: <EditBackupExclusionsPage /> },
  { path: '/backup/incremental', element: <IncrementalBackupsPage /> },
  { path: '/backup/incremental/:snapshot', element: <SnapshotDetailPage /> },
  { path: '/backup/incremental/:snapshot/files', element: <SnapshotFilesPage /> },
  { path: '/backup/:backup', element: <BackupDetailPage /> },
]

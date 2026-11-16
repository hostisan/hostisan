import type { RouteObject } from 'react-router'
import { AddCronJobPage, EditCronJobPage } from './CronFormPage'
import { CronJobsPage } from './CronJobsPage'

export const routes: RouteObject[] = [
  { path: '/cron', element: <CronJobsPage /> },
  { path: '/cron/new', element: <AddCronJobPage /> },
  { path: '/cron/:id', element: <EditCronJobPage /> },
]

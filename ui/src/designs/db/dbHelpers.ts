import type { Database } from '../../api/hestia.gen'

export const dbTypeLabel = (type: Database['TYPE']) => (type === 'mysql' ? 'MySQL' : 'PostgreSQL')

export const dbAdminName = (type: Database['TYPE']) => (type === 'mysql' ? 'phpMyAdmin' : 'phpPgAdmin')

import { Switch, Text } from '@cloudflare/kumo'
import type { CronJobs } from '../../api/types.gen'
import { useCronOptions } from '../../api/cron'
import {
  AsideCard,
  AsideSection,
  DetailRow,
  PageAside,
  StatGrid,
  StatTile,
  UsageMeter,
} from '../../components/PageAside'
import { useT } from '../../i18n/I18nProvider'
import { useSessionUser } from '../../session/SessionProvider'
import { formatServerTime } from './describeCron'

type CronAsideProps = {
  data?: CronJobs
  onReportsChange: (on: boolean) => void
  busy?: boolean
}

export function CronAside({ data, onReportsChange, busy }: CronAsideProps) {
  const t = useT()
  const user = useSessionUser()
  const options = useCronOptions().data
  const jobs = data?.jobs ?? []
  const readOnly = user?.readOnly ?? false

  return (
    <PageAside>
      <AsideSection title={t('Usage')}>
        <UsageMeter
          label={
            Number(data?.usage.suspended) > 0
              ? t('Cron jobs (%s suspended)', data?.usage.suspended ?? '')
              : t('Cron jobs')
          }
          used={Number(data?.usage.jobs || jobs.length)}
          limit={data?.usage.limit || 'unlimited'}
        />
      </AsideSection>
      <StatGrid>
        <StatTile label={t('Active')} value={jobs.filter((j) => j.SUSPENDED === 'no').length} />
        <StatTile label={t('Suspended')} value={jobs.filter((j) => j.SUSPENDED === 'yes').length} />
      </StatGrid>
      {options && (
        <AsideCard title={t('Server time')} description={t("Schedules run in the server's time zone.")}>
          <DetailRow label={t('Now')}>
            <Text as="span">{formatServerTime(options.now, options.timezone, t)}</Text>
          </DetailRow>
          <DetailRow label={t('Time zone')}>
            <Text as="span">{options.timezone}</Text>
          </DetailRow>
        </AsideCard>
      )}
      <AsideCard
        title={t('Cron notifications')}
        description={t('Email the output of every job to %s.', data?.contact || user?.contact || t('your contact address'))}
      >
        <Switch
          aria-label={t('Cron notifications')}
          checked={data?.reports ?? false}
          disabled={readOnly || busy || !data}
          onCheckedChange={onReportsChange}
        />
      </AsideCard>
    </PageAside>
  )
}

import { Badge, Banner, Loader, RefreshButton, Switch, Table, Text } from '@cloudflare/kumo'
import { PackageIcon, WarningCircleIcon } from '@phosphor-icons/react'
import { useState } from 'react'
import { apiErrorText } from '../../api/errors'
import { useServerMutation, useUpdates } from '../../api/server'
import { PageHeader } from '../../components/PageHeader'
import { TableCard } from '../../components/table'
import { useT } from '../../i18n/I18nProvider'

export function UpdatesPage() {
  const t = useT()
  const { data, error, refetch, isFetching } = useUpdates()
  const { setAutoupdate } = useServerMutation()
  const [switching, setSwitching] = useState(false)
  const [switchError, setSwitchError] = useState<unknown>(null)

  const toggle = async (enabled: boolean) => {
    setSwitching(true)
    setSwitchError(null)
    try {
      await setAutoupdate({ enabled })
    } catch (e) {
      setSwitchError(e)
    } finally {
      setSwitching(false)
    }
  }

  const failure = error ?? switchError
  const rows = data?.packages ?? []
  const pending = rows.filter((p) => !p.updated).length
  const auto = data?.autoupdate ?? false

  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Updates')}
        description={t('Packages installed on this server and whether a newer version is available.')}
        actions={
          <>
            <RefreshButton aria-label={t('Refresh package list')} loading={isFetching} onClick={() => void refetch()} />
            <div className="flex h-9 items-center rounded-lg px-3 ring ring-kumo-line">
              <Switch
                label={t('Automatic updates')}
                checked={auto}
                disabled={!data || switching}
                onCheckedChange={(v) => void toggle(v)}
              />
            </div>
          </>
        }
      />

      {failure ? (
        <div role="alert">
          <Banner variant="error" icon={<WarningCircleIcon />} title={apiErrorText(failure, t)} />
        </div>
      ) : null}

      {data && pending > 0 && (
        <Banner variant="default">
          {pending === 1
            ? t('1 package has an update available.')
            : t('%d packages have an update available.', pending)}{' '}
          {auto
            ? t('It will be installed during the next automatic update run.')
            : t('Install it with apt, or turn on automatic updates.')}
        </Banner>
      )}

      {error ? null : !data ? (
        <Loader />
      ) : (
        <TableCard
          noun={{ one: t('package'), other: t('packages') }}
          filteredCount={rows.length}
          totalCount={rows.length}
          emptyState={{
            icon: <PackageIcon size={40} className="text-kumo-inactive" />,
            title: t('No packages found'),
            description: t('The package list could not be read from apt.'),
          }}
        >
          <Table className="whitespace-nowrap">
            <Table.Header>
              <Table.Row>
                <Table.Head sticky="left">{t('Package name')}</Table.Head>
                <Table.Head>{t('Description')}</Table.Head>
                <Table.Head>{t('Version')}</Table.Head>
                <Table.Head>{t('Status')}</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {rows.map((p) => (
                <Table.Row key={p.name}>
                  <Table.Cell sticky="left">
                    <Text bold as="span">
                      {p.displayName}
                    </Text>
                  </Table.Cell>
                  <Table.Cell>
                    <Text variant="secondary" as="span">
                      {p.description}
                    </Text>
                  </Table.Cell>
                  <Table.Cell>
                    <Text variant="mono" as="span">
                      {p.version}
                    </Text>{' '}
                    <Text variant="secondary" size="sm" as="span">
                      ({p.arch})
                    </Text>
                  </Table.Cell>
                  <Table.Cell>
                    {p.updated ? (
                      <Badge variant="success" appearance="dot">
                        {t('Package up-to-date')}
                      </Badge>
                    ) : (
                      <Badge variant="warning" appearance="dot">
                        {t('Update available')}
                      </Badge>
                    )}
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </TableCard>
      )}
    </div>
  )
}

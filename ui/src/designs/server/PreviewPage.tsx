import { Badge, Banner, Loader, Table, Text } from '@cloudflare/kumo'
import { FlaskIcon, WarningCircleIcon } from '@phosphor-icons/react'
import { apiErrorText } from '../../api/errors'
import { usePreview } from '../../api/server'
import { PageHeader } from '../../components/PageHeader'
import { TableCard } from '../../components/table'
import { useT } from '../../i18n/I18nProvider'

/** The preview features list (decision 49): admin-only, the same static rows as the API sends. */
export function PreviewPage() {
  const t = useT()
  const { data, error } = usePreview()
  const rows = data?.rows ?? []
  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Preview features')}
        description={t('Features that are still being ported and what works so far.')}
        back={{ href: '/server/config', label: t('Configure server') }}
      />
      {error ? (
        <div role="alert">
          <Banner variant="error" icon={<WarningCircleIcon />} title={apiErrorText(error, t)} />
        </div>
      ) : !data ? (
        <Loader />
      ) : (
        <TableCard
          noun={t('features')}
          filteredCount={rows.length}
          totalCount={rows.length}
          emptyState={{ icon: <FlaskIcon size={40} className="text-kumo-inactive" />, title: t('No preview features') }}
        >
          <Table className="whitespace-nowrap">
            <Table.Header>
              <Table.Row>
                <Table.Head sticky="left">{t('Category')}</Table.Head>
                <Table.Head>{t('Name')}</Table.Head>
                <Table.Head>{t('Status')}</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {rows.map((r) => (
                <Table.Row key={`${r.category}-${r.name}`}>
                  <Table.Cell sticky="left">
                    <Text as="span">{t(r.category)}</Text>
                  </Table.Cell>
                  <Table.Cell>
                    <Text as="span">{t(r.name)}</Text>
                  </Table.Cell>
                  <Table.Cell>
                    <Badge variant="warning" appearance="dot">
                      {t(r.status)}
                    </Badge>
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

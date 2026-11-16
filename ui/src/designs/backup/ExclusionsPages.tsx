import { InputArea, LayerCard, LinkButton, Table, Text } from '@cloudflare/kumo'
import { PencilSimpleIcon } from '@phosphor-icons/react'
import { useKumoToastManager } from '@cloudflare/kumo'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import { useBackupExclusions, useBackupMutation } from '../../api/backup'
import type { BackupExclusions } from '../../api/hestia.gen'
import { FormPage } from '../../components/FormPage'
import { FormSection } from '../../components/FormSection'
import { PageHeader } from '../../components/PageHeader'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSessionUser } from '../../session/SessionProvider'
import { DnsError, DnsLoading } from '../dns/shared'
import { BackupTabs } from './BackupTabs'
import { interpolate } from '../../lib/interpolate'

type Category = keyof BackupExclusions

const categoryLabels: Record<Category, string> = {
  WEB: msgid('Web domains'),
  DNS: msgid('DNS zones'),
  MAIL: msgid('Mail domains'),
  DB: msgid('Databases'),
  CRON: msgid('Cron jobs'),
  USER: msgid('User directories'),
}

const categories: Category[] = ['WEB', 'DNS', 'MAIL', 'DB', 'CRON', 'USER']

export function BackupExclusionsPage() {
  const t = useT()
  const q = useBackupExclusions()
  const readOnly = useSessionUser()?.readOnly ?? false
  return (
    <div className="grid gap-6">
      <PageHeader
        title={t('Backups')}
        description={t("Domains, databases and directories left out of this account's backups.")}
        actions={
          !readOnly && (
            <LinkButton href="/backup/exclusions/edit" variant="primary" icon={PencilSimpleIcon}>
              {t('Edit backup exclusions')}
            </LinkButton>
          )
        }
      />
      <BackupTabs value="/backup/exclusions" />

      {q.isPending ? (
        <DnsLoading />
      ) : q.isError ? (
        <DnsError error={q.error} />
      ) : (
        <LayerCard className="p-0">
          <div className="overflow-x-auto">
            <Table className="whitespace-nowrap">
              <Table.Header>
                <Table.Row>
                  <Table.Head>{t('Type')}</Table.Head>
                  <Table.Head className="w-full">{t('Value')}</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {categories.map((c) => {
                  const entries = q.data.exclusions[c]
                  return (
                    <Table.Row key={c}>
                      <Table.Cell className="align-top">
                        <Text bold as="span">
                          {t(categoryLabels[c])}
                        </Text>
                      </Table.Cell>
                      <Table.Cell>
                        {entries.length === 0 ? (
                          <Text variant="secondary" as="span">
                            {t('No exclusions')}
                          </Text>
                        ) : (
                          <ul className="grid gap-1">
                            {entries.map(({ Key, Value }, i) => (
                              <li key={`${Key}:${i}`} className="font-mono text-[0.9em]">
                                <span className="font-medium">{Key}</span>
                                {Value ? (
                                  <span className="text-kumo-subtle"> {Value.split(',').join(', ')}</span>
                                ) : (
                                  <span className="font-sans text-[1.1em] text-kumo-subtle"> {t('(everything)')}</span>
                                )}
                              </li>
                            ))}
                          </ul>
                        )}
                      </Table.Cell>
                    </Table.Row>
                  )
                })}
              </Table.Body>
            </Table>
          </div>
        </LayerCard>
      )}
    </div>
  )
}

export function EditBackupExclusionsPage() {
  const q = useBackupExclusions()
  if (q.isPending) return <DnsLoading />
  if (q.isError) return <DnsError error={q.error} />
  const t = q.data.text
  return <ExclusionsForm initial={{ web: t.web, mail: t.mail, db: t.db, userdir: t.userdir }} />
}

function ExclusionsForm({ initial }: { initial: { web: string; mail: string; db: string; userdir: string } }) {
  const [web, setWeb] = useState(initial.web)
  const [mail, setMail] = useState(initial.mail)
  const [db, setDb] = useState(initial.db)
  const [userdir, setUserdir] = useState(initial.userdir)
  const [error, setError] = useState<unknown>(null)
  const readOnly = useSessionUser()?.readOnly ?? false
  const m = useBackupMutation()
  const toast = useKumoToastManager()
  const t = useT()
  const navigate = useNavigate()

  return (
    <FormPage
      readOnly={readOnly}
      onSubmit={() => {
        setError(null)
        m.saveExclusions({ web, mail, db, userdir }).then(() => {
          toast.add({ title: t('Changes have been saved.'), type: 'success' })
          navigate('/backup/exclusions')
        }, setError)
      }}
      title={t('Edit backup exclusions')}
      description={t('One entry per line. Excluded items are skipped by every scheduled and manual backup.')}
      back={{ href: '/backup/exclusions', label: t('Backup exclusions') }}
    >
      {error != null && <DnsError error={error} />}
      <FormSection
        title={t('Web domains')}
        description={interpolate(
          t('To exclude specific directories use %s'),
          <span className="font-mono text-[0.9em] break-all">domain.tld:public_html/cache:public_html/tmp</span>,
        )}
      >
        <InputArea
          label={t('Web domains')}
          value={web}
          onValueChange={setWeb}
          rows={5}
          className="font-mono"
          placeholder={t(
            'Type domain name, one per line. To exclude all domains use *. To exclude specific dirs use following format: domain.tld:public_html/cache:public_html/tmp',
          )}
        />
      </FormSection>

      <FormSection
        title={t('Mail domains')}
        description={interpolate(
          t('To exclude specific accounts use %s'),
          <span className="font-mono text-[0.9em] break-all">domain.tld:info:support:postmaster</span>,
        )}
      >
        <InputArea
          label={t('Mail domains')}
          value={mail}
          onValueChange={setMail}
          rows={5}
          className="font-mono"
          placeholder={t(
            'Type domain name, one per line. To exclude all domains use *. To exclude specific accounts use following format: domain.tld:info:support:postmaster',
          )}
        />
      </FormSection>

      <FormSection title={t('Databases')} description={t('Use the full database name, including the user prefix.')}>
        <InputArea
          label={t('Databases')}
          value={db}
          onValueChange={setDb}
          rows={5}
          className="font-mono"
          placeholder={t('Type full database name, one per line. To exclude all databases use *')}
        />
      </FormSection>

      <FormSection title={t('User directory')} description={t('Directories inside the home folder, relative to it.')}>
        <InputArea
          label={t('User directory')}
          value={userdir}
          onValueChange={setUserdir}
          rows={5}
          className="font-mono"
          placeholder={t('Type directory name, one per line. To exclude all dirs use *')}
        />
      </FormSection>
    </FormPage>
  )
}

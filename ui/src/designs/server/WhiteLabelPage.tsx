import { Banner, Checkbox, Input, Loader, Select, Text, useKumoToastManager } from '@cloudflare/kumo'
import { WarningCircleIcon } from '@phosphor-icons/react'
import { useState } from 'react'
import { apiErrorText } from '../../api/errors'
import { useServerMutation, useWhiteLabel } from '../../api/server'
import type { WhiteLabelView } from '../../api/types.gen'
import { FormPage } from '../../components/FormPage'
import { FormSection } from '../../components/FormSection'
import { PageHeader } from '../../components/PageHeader'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSession } from '../../session/SessionProvider'
import { interpolate } from '../../lib/interpolate'

const back = { href: '/server/config', label: msgid('Server configuration') }

const customLogoDir = '/usr/local/hestia/web/images/custom/'

const logoFiles = [
  { file: 'logo.svg', size: '100px x 120px' },
  { file: 'logo.png', size: '100px x 120px' },
  { file: 'logo-header.svg', size: '54px x 29px' },
  { file: 'favicon.png', size: '64px x 64px' },
  { file: 'favicon.ico', size: '16px x 16px' },
]

function Variables({ names }: { names: string[] }) {
  const t = useT()
  return (
    <>
      {t('Supported variables')}:{' '}
      {names.map((n, i) => (
        <span key={n}>
          <span className="font-mono text-[0.9em]">{`{{${n}}}`}</span>
          {i < names.length - 1 ? ', ' : ''}
        </span>
      ))}
    </>
  )
}

function WhiteLabelForm({ view }: { view: WhiteLabelView }) {
  const t = useT()
  const toast = useKumoToastManager()
  const { refresh } = useSession()
  const { saveWhiteLabel } = useServerMutation()
  const [appName, setAppName] = useState(view.appName)
  const [title, setTitle] = useState(view.title)
  const [fromName, setFromName] = useState(view.fromName)
  const [fromEmail, setFromEmail] = useState(view.fromEmail)
  const [subjectEmail, setSubjectEmail] = useState(view.subjectEmail)
  const [hideDocs, setHideDocs] = useState(view.hideDocs)
  const [updateLogo, setUpdateLogo] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<unknown>(null)

  const submit = async () => {
    setSaving(true)
    setError(null)
    try {
      await saveWhiteLabel({ appName, title, fromName, fromEmail, subjectEmail, hideDocs, updateLogo })
      toast.add({ title: t('Changes have been saved.'), type: 'success' })
      setUpdateLogo(false)
      // The brand name comes from the bootstrap, so reload it to show the new one.
      await refresh()
    } catch (e) {
      setError(e)
    } finally {
      setSaving(false)
    }
  }

  return (
    <FormPage
      title={t('White label options')}
      description={t('Rebrand the panel and the emails it sends.')}
      back={{ href: back.href, label: t(back.label) }}
      submitDisabled={saving}
      onSubmit={() => void submit()}
    >
      {error ? (
        <div role="alert">
          <Banner variant="error" icon={<WarningCircleIcon />} title={apiErrorText(error, t)} />
        </div>
      ) : null}
      <FormSection
        title={t('General')}
        description={t('Names shown in the browser, on the login screen and in emails.')}
      >
        <Input label={t('Application name')} value={appName} onChange={(e) => setAppName(e.target.value)} />
        <Input
          label={t('Title')}
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          description={<Variables names={['appname', 'hostname', 'ip', 'page']} />}
          className="font-mono"
        />
        <div className="grid gap-4 sm:grid-cols-2">
          <Input
            label={t('Sender name')}
            value={fromName}
            onChange={(e) => setFromName(e.target.value)}
            description={t('Default: %s', appName || view.appName)}
          />
          <Input
            label={t('Sender email address')}
            type="email"
            value={fromEmail}
            onChange={(e) => setFromEmail(e.target.value)}
            description={t('Default: %s', `noreply@${view.hostname}`)}
          />
        </div>
        <Input
          label={t('Email subject')}
          value={subjectEmail}
          onChange={(e) => setSubjectEmail(e.target.value)}
          description={<Variables names={['appname', 'hostname', 'subject']} />}
          className="font-mono"
        />
        <Select
          label={t('Hide link to documentation')}
          value={hideDocs ? 'yes' : 'no'}
          onValueChange={(v) => setHideDocs(v === 'yes')}
          items={{ yes: t('Yes'), no: t('No') }}
          className="w-full sm:max-w-xs"
        />
      </FormSection>

      <FormSection
        title={t('Custom logo')}
        description={
          <>
            {interpolate(
              t('Upload the files to %s, then tick Update logo and save.'),
              <span className="font-mono text-[0.9em]">{customLogoDir}</span>,
            )}
          </>
        }
      >
        <ul className="grid gap-2">
          {logoFiles.map((l) => (
            <li key={l.file} className="flex flex-wrap items-baseline justify-between gap-x-4">
              <Text variant="mono" as="span">
                {l.file}
              </Text>
              <Text variant="secondary" size="sm" as="span">
                {l.size}
              </Text>
            </li>
          ))}
        </ul>
        <div className="grid gap-2 border-t border-kumo-line pt-4">
          <Checkbox label={t('Update logo')} checked={updateLogo} onCheckedChange={(v) => setUpdateLogo(v === true)} />
        </div>
      </FormSection>
    </FormPage>
  )
}

export function WhiteLabelPage() {
  const t = useT()
  const { data, error } = useWhiteLabel()
  if (error) {
    return (
      <div className="grid gap-6">
        <PageHeader title={t('White label options')} back={{ href: back.href, label: t(back.label) }} />
        <div role="alert">
          <Banner variant="error" icon={<WarningCircleIcon />} title={apiErrorText(error, t)} />
        </div>
      </div>
    )
  }
  if (!data) return <Loader />
  return <WhiteLabelForm view={data} />
}

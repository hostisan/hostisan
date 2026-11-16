import { Banner, Button, Empty, Input, Loader, LinkButton, Tabs, Text } from '@cloudflare/kumo'
import { GearIcon, PencilSimpleIcon, WarningCircleIcon } from '@phosphor-icons/react'
import { useCallback, useState } from 'react'
import { useParams } from 'react-router'
import { ApiError } from '../../api/client'
import { apiErrorText } from '../../api/errors'
import { useEditor, useServerMutation } from '../../api/server'
import type { EditorSaveResult, EditorView, JobStatus } from '../../api/types.gen'
import { FormPage } from '../../components/FormPage'
import { FormSection } from '../../components/FormSection'
import { PageHeader } from '../../components/PageHeader'
import { useT } from '../../i18n/I18nProvider'
import { JobProgress } from './JobProgress'
import { sentenceCase } from './format'
import { msgid } from '../../i18n/msgid'
import { ConfigEditor, RestartCheckbox } from './parts'

const READ_ONLY_INFO = msgid('Info (read-only mode): Crontab can only be edited via SSH.')
const UNTESTED_WARNING = msgid(
  'Nothing will test the new configuration if the service is not restarted. A mistake can stop it from starting the next time it restarts.',
)
const UNTESTED_SAVED = msgid(
  'Changes have been saved, but nothing tested the new configuration. Restart the service to check that it starts.',
)

/** The signature of what is on disk, so the form resets only when a file actually changed. */
const signature = (view: EditorView) => `${view.service}:${view.files.map((f) => f.hash).join(',')}`

function EditorForm({ view, onReload }: { view: EditorView; onReload: () => void }) {
  const t = useT()
  const { saveEditor } = useServerMutation()
  const hasBasic = view.directives !== undefined && Object.keys(view.directives).length > 0

  const initialTexts = () => Object.fromEntries(view.files.map((f) => [f.id, f.content]))
  const [seen, setSeen] = useState(signature(view))
  const [texts, setTexts] = useState<Record<string, string>>(initialTexts)
  const [dirs, setDirs] = useState<Record<string, string>>(view.directives ?? {})
  const [mode, setMode] = useState(hasBasic ? 'basic' : 'advanced')
  const [restart, setRestart] = useState(true)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<unknown>(null)
  const [jobId, setJobId] = useState('')
  const [outcome, setOutcome] = useState<JobStatus | null>(null)

  // The files changed on disk (our own save, or another admin): show what is there now.
  if (signature(view) !== seen) {
    setSeen(signature(view))
    setTexts(initialTexts())
    setDirs(view.directives ?? {})
  }

  const onDone = useCallback((status: JobStatus) => setOutcome(status), [])

  const readable = view.files.filter((f) => !f.error)
  const basic = hasBasic && mode === 'basic'
  const changedDirectives = Object.fromEntries(
    Object.entries(dirs).filter(([k, v]) => v !== (view.directives ?? {})[k]),
  )
  const changed = basic ? Object.keys(changedDirectives).length > 0 : readable.some((f) => texts[f.id] !== f.content)
  const dirty = changed
  const blocked = view.readOnly || !!view.saveBlocked || view.files.some((f) => f.error)
  const running = saving || (jobId !== '' && outcome === null)

  const submit = async () => {
    setSaving(true)
    setSaveError(null)
    setOutcome(null)
    setJobId('')
    try {
      const ref = await saveEditor(view.service, {
        files: basic ? [] : readable.map((f) => ({ id: f.id, content: texts[f.id], hash: f.hash })),
        ...(basic ? { directives: changedDirectives } : {}),
        restart: view.restart ? restart : false,
      })
      setJobId(ref.jobId)
    } catch (e) {
      setSaveError(e)
    } finally {
      setSaving(false)
    }
  }

  const panelCron = view.family === 'panel-cron'
  const back = panelCron
    ? { href: '/server/config', label: t('Configure server') }
    : { href: '/server/services', label: t('Services') }
  // The default php.ini editor, offered next to the web server editors.
  const showPhpLink = ['nginx', 'apache2', 'httpd'].includes(view.service)
  const result = outcome?.state === 'done' ? (outcome.result as EditorSaveResult | null) : null
  const restartFailed = outcome?.state === 'failed' && outcome.error?.code === 'restart_failed'
  const files = view.files
  const stateArg = outcome?.error?.args?.[0] ?? ''
  const serviceState = stateArg === 'running' ? t('Running') : stateArg === 'stopped' ? t('Stopped') : ''

  return (
    <FormPage
      title={`${t('Configure server')}: ${sentenceCase(view.title)}`}
      back={back}
      readOnly={view.readOnly}
      submitDisabled={blocked || !changed || running}
      onSubmit={() => void submit()}
      actions={
        showPhpLink ? (
          <LinkButton href="/server/services/php" variant="secondary" icon={PencilSimpleIcon}>
            {t('Configure PHP')}
          </LinkButton>
        ) : undefined
      }
    >
      {view.readOnly && <Banner variant="default">{t(READ_ONLY_INFO)}</Banner>}
      {view.saveBlocked && <Banner variant="alert" icon={<WarningCircleIcon />} title={t(view.saveBlocked)} />}
      {saveError ? (
        <div role="alert">
          <Banner
            variant="error"
            icon={<WarningCircleIcon />}
            title={apiErrorText(saveError, t)}
            className="whitespace-pre-line"
          />
          {saveError instanceof ApiError && saveError.code === 'changed' && (
            <div className="pt-2">
              <Button
                onClick={() => {
                  setSaveError(null)
                  onReload()
                }}
              >
                {t('Reload file')}
              </Button>
            </div>
          )}
        </div>
      ) : null}

      {hasBasic && (
        <Tabs
          variant="segmented"
          value={mode}
          onValueChange={(v) => {
            // Switching would drop the edits made in the other mode.
            if (!dirty) setMode(v)
          }}
          tabs={[
            { value: 'basic', label: t('Basic options') },
            { value: 'advanced', label: t('Advanced options') },
          ]}
          className="w-fit"
        />
      )}

      {hasBasic && dirty && (
        <Text variant="secondary" size="sm">
          {t('Save your changes before switching between basic and advanced options.')}
        </Text>
      )}

      {basic ? (
        <FormSection title={t('Basic options')} description={t('Common directives from the configuration file.')}>
          <div className="grid gap-4 sm:grid-cols-2">
            {Object.entries(dirs).map(([key, value]) => (
              <Input
                key={key}
                label={<span className="font-mono text-[0.9em]">{key}</span>}
                value={value}
                onChange={(e) => setDirs((d) => ({ ...d, [key]: e.target.value }))}
                spellCheck={false}
              />
            ))}
          </div>
        </FormSection>
      ) : (
        <FormSection
          title={files.length > 1 ? t('Configuration files') : t('Configuration file')}
          description={t('Edit the file directly. Invalid syntax can stop the service from starting.')}
        >
          {files.map((f) => (
            <ConfigEditor
              key={f.id}
              path={f.path}
              value={texts[f.id] ?? ''}
              onChange={(text) => setTexts((x) => ({ ...x, [f.id]: text }))}
              error={f.error}
              readOnly={view.readOnly}
              rows={files.length > 2 ? 10 : 22}
            />
          ))}
        </FormSection>
      )}

      {view.restart && !view.readOnly && (
        <div className="grid gap-3">
          <RestartCheckbox checked={restart} onCheckedChange={setRestart} />
          {/* Services with a checker (nginx -t, sshd -t...) are tested on every save. */}
          {!restart && !view.syntaxCheck && (
            <Banner variant="alert" icon={<WarningCircleIcon />} title={t(UNTESTED_WARNING)} />
          )}
        </div>
      )}

      {jobId && <JobProgress jobId={jobId} onDone={onDone} />}
      {restartFailed && serviceState && (
        <div role="alert">
          <Banner variant="error" icon={<WarningCircleIcon />} title={t('Service state: %s', serviceState)} />
        </div>
      )}
      {result &&
        (result.untested ? (
          <Banner variant="alert" icon={<WarningCircleIcon />} title={t(UNTESTED_SAVED)} />
        ) : (
          <Banner variant="default">
            {result.changed.length > 0
              ? t('Changes have been saved.')
              : t('Nothing changed: the files are already as typed.')}
          </Banner>
        ))}
    </FormPage>
  )
}

export function ServiceEditPage() {
  const t = useT()
  const { service = '' } = useParams()
  const { data, error, refetch } = useEditor(service)
  const back = { href: '/server/services', label: t('Services') }

  if (error) {
    const missing = error instanceof ApiError && error.status === 404
    return (
      <div className="grid gap-6">
        <PageHeader title={missing ? t('Service not found') : t('Configure server')} back={back} />
        {missing ? (
          <Empty
            icon={<GearIcon size={40} className="text-kumo-inactive" />}
            title={t('There is no service called %s', service)}
            description={t('It may have been removed or renamed. Pick a service from the list.')}
            contents={<LinkButton href="/server/services">{t('Back to services')}</LinkButton>}
            className="py-16"
          />
        ) : (
          <div role="alert">
            <Banner variant="error" icon={<WarningCircleIcon />} title={apiErrorText(error, t)} />
          </div>
        )}
      </div>
    )
  }
  if (!data) return <Loader />
  return <EditorForm key={data.service} view={data} onReload={() => void refetch()} />
}

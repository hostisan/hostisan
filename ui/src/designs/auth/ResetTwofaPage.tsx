import { Banner, Button, Input, Text } from '@cloudflare/kumo'
import { ArrowLeftIcon, CheckCircleIcon, LockOpenIcon, SignInIcon, WarningCircleIcon } from '@phosphor-icons/react'
import { useState, type FormEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { useRecovery } from '../../api/account'
import { apiErrorText } from '../../api/errors'
import { PasswordInput } from '../../components/PasswordInput'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { AuthLayout, UsernameChip } from './AuthLayout'

type Step = 'request' | 'code' | 'done'

/** Email links: ?action=code|confirm&user=…&code=… open the code step. */
function initialStep(action: string | null, user: string): Step {
  return (action === 'code' || action === 'confirm') && user ? 'code' : 'request'
}

export function ResetTwofaPage() {
  const navigate = useNavigate()
  const t = useT()
  const { requestUnlock, confirmUnlock } = useRecovery()
  const [params] = useSearchParams()
  const [username, setUsername] = useState(params.get('user') ?? '')
  const [step, setStep] = useState<Step>(() => initialStep(params.get('action'), username))
  const [password, setPassword] = useState('')
  const [code, setCode] = useState(params.get('code') ?? '')
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [busy, setBusy] = useState(false)

  const goTo = (next: Step) => {
    setError('')
    setNotice('')
    setStep(next)
  }

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setError('')
    setBusy(true)
    try {
      if (step === 'request') {
        await requestUnlock({ user: username.trim(), password })
        setPassword('')
        setStep('code')
        setNotice(msgid('If the details are correct, an unlock code has been sent to the email address on your account.'))
      } else {
        await confirmUnlock({ user: username.trim(), code: code.trim() })
        setStep('done')
      }
    } catch (err) {
      setError(apiErrorText(err, (m) => m))
    } finally {
      setBusy(false)
    }
  }

  if (step === 'done') {
    return (
      <AuthLayout title={t('Account unlocked')}>
        <div className="grid gap-4">
          <div className="flex items-start gap-2">
            <span className="flex h-lh items-center">
              <LockOpenIcon className="text-kumo-success" weight="fill" />
            </span>
            <Text>
              {t('Two-factor authentication is now turned off for your account. You may now proceed to log in.')}
            </Text>
          </div>
          <div>
            <Button variant="primary" icon={SignInIcon} onClick={() => navigate('/login')}>
              {t('Log in')}
            </Button>
          </div>
        </div>
      </AuthLayout>
    )
  }

  return (
    <AuthLayout
      title={t('Unlock account')}
      description={
        step === 'request'
          ? t('Lost your authenticator? Enter your username and password and we will email you an unlock code.')
          : t('Enter the unlock code from the email we sent you.')
      }
    >
      <form onSubmit={submit} className="grid gap-4">
        {error && (
          <Banner variant="error" icon={<WarningCircleIcon weight="fill" />}>
            {t(error)}
          </Banner>
        )}
        {notice && !error && (
          <Banner variant="default" icon={<CheckCircleIcon weight="fill" />}>
            {t(notice)}
          </Banner>
        )}

        {step === 'request' ? (
          <>
            <Input
              label={t('Username')}
              name="user"
              autoComplete="username"
              autoFocus
              required
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
            <PasswordInput
              label={t('Password')}
              name="password"
              isNew={false}
              autoComplete="current-password"
              required
              value={password}
              onChange={setPassword}
            />
          </>
        ) : (
          <>
            <UsernameChip username={username} onChange={() => goTo('request')} />
            <Input
              label={t('Unlock code')}
              name="code"
              autoComplete="one-time-code"
              autoFocus
              required
              className="font-mono"
              value={code}
              onChange={(e) => setCode(e.target.value)}
            />
          </>
        )}

        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" variant="primary" loading={busy}>
            {t('Submit')}
          </Button>
          <Button
            type="button"
            variant="ghost"
            icon={ArrowLeftIcon}
            onClick={() => (step === 'code' ? goTo('request') : navigate('/login'))}
          >
            {t('Back')}
          </Button>
        </div>
      </form>
    </AuthLayout>
  )
}

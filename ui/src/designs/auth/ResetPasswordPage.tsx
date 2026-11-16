import { Banner, Button, Input, Text } from '@cloudflare/kumo'
import { ArrowLeftIcon, CheckCircleIcon, SignInIcon, WarningCircleIcon } from '@phosphor-icons/react'
import { useState, type FormEvent } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { useRecovery } from '../../api/account'
import { apiErrorText } from '../../api/errors'
import { PasswordInput } from '../../components/PasswordInput'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { AuthLayout, UsernameChip } from './AuthLayout'

type Step = 'request' | 'code' | 'password' | 'done'

/** Errors that mean the code, not the password, has to be entered again. */
const codeErrors = new Set(['Invalid username or code', 'Code has been expired'])

/** Hestia-format email links: ?action=code&user=… or ?action=confirm&user=…&code=… */
function initialStep(action: string | null, user: string, code: string): Step {
  if (action === 'confirm' && user && code) return 'password'
  if (action === 'code' && user) return 'code'
  return 'request'
}

export function ResetPasswordPage() {
  const navigate = useNavigate()
  const t = useT()
  const { requestReset, confirmReset } = useRecovery()
  const [params] = useSearchParams()
  const [username, setUsername] = useState(params.get('user') ?? '')
  const [code, setCode] = useState(params.get('code') ?? '')
  const [step, setStep] = useState<Step>(() => initialStep(params.get('action'), username, code))
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
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
        await requestReset({ user: username.trim(), email: email.trim() })
        setStep('code')
        setNotice(msgid('Password reset instructions have been sent to the email address associated with this account.'))
      } else if (step === 'code') {
        setStep('password')
      } else if (step === 'password') {
        await confirmReset({ user: username.trim(), code: code.trim(), password, passwordConfirm: confirm })
        setStep('done')
      }
    } catch (err) {
      const text = apiErrorText(err, (m) => m)
      setError(text)
      if (step === 'password' && codeErrors.has(text)) setStep('code')
    } finally {
      setBusy(false)
    }
  }

  if (step === 'done') {
    return (
      <AuthLayout title={t('Password changed')}>
        <div className="grid gap-4">
          <div className="flex items-start gap-2">
            <span className="flex h-lh items-center">
              <CheckCircleIcon className="text-kumo-success" weight="fill" />
            </span>
            <Text>{t('Your password has been changed. You may now log in with it.')}</Text>
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
      title={t('Forgot your password?')}
      description={
        step === 'request'
          ? t('Enter your username and the email address on your account. We will send you a reset code.')
          : step === 'code'
            ? t('Enter the reset code from the email we sent you.')
            : t('Choose a new password for your account.')
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

        {step === 'request' && (
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
            <Input
              label={t('Email')}
              type="email"
              name="email"
              autoComplete="email"
              required
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </>
        )}

        {step !== 'request' && <UsernameChip username={username} onChange={() => goTo('request')} />}

        {step === 'code' && (
          <Input
            label={t('Reset code')}
            name="code"
            autoComplete="one-time-code"
            autoFocus
            required
            className="font-mono"
            value={code}
            onChange={(e) => setCode(e.target.value)}
          />
        )}

        {step === 'password' && (
          <>
            <PasswordInput
              label={t('New password')}
              name="password"
              autoFocus
              required
              value={password}
              onChange={setPassword}
              onGenerate={setConfirm}
            />
            <PasswordInput
              label={t('Confirm password')}
              name="password_confirm"
              isNew={false}
              autoComplete="new-password"
              required
              value={confirm}
              onChange={setConfirm}
            />
          </>
        )}

        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" variant="primary" loading={busy}>
            {step === 'request' ? t('Submit') : step === 'code' ? t('Confirm') : t('Reset')}
          </Button>
          <Button
            type="button"
            variant="ghost"
            icon={ArrowLeftIcon}
            onClick={() => (step === 'password' ? goTo('code') : step === 'code' ? goTo('request') : navigate('/login'))}
          >
            {t('Back')}
          </Button>
        </div>
      </form>
    </AuthLayout>
  )
}

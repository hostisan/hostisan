import { PasswordInput } from '../../components/PasswordInput'
import { Banner, Button, Input, Link } from '@cloudflare/kumo'
import { ArrowLeftIcon, SignInIcon, WarningCircleIcon } from '@phosphor-icons/react'
import { useState, type FormEvent } from 'react'
import { Navigate, useNavigate } from 'react-router'
import { api, ApiError } from '../../api/client'
import type { LoginRequest, StatusReply } from '../../api/types.gen'
import { useT } from '../../i18n/I18nProvider'
import { msgid } from '../../i18n/msgid'
import { useSession } from '../../session/SessionProvider'
import { AuthLayout, UsernameChip } from './AuthLayout'

type Step = 'username' | 'password' | 'twofa'

export function LoginPage() {
  const navigate = useNavigate()
  const t = useT()
  const { bootstrap, status, refresh, signedOutReason, setSignedOutReason } = useSession()
  const [step, setStep] = useState<Step>('username')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [token, setToken] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const canReset = bootstrap?.passwordReset === true

  if (status === 'ready' && bootstrap?.authenticated && bootstrap.user) {
    return <Navigate to={bootstrap.user.landing} replace />
  }

  const goTo = (next: Step) => {
    setError('')
    setStep(next)
  }

  const restart = () => {
    setPassword('')
    setToken('')
    goTo('username')
  }

  const send = async (body: LoginRequest) => {
    setBusy(true)
    setError('')
    setSignedOutReason('')
    try {
      const reply = await api.post<StatusReply>('/auth/login', body)
      if (reply.status === 'twofa_required') {
        goTo('twofa')
        return
      }
      await refresh()
      navigate(reply.landing ?? '/', { replace: true })
    } catch (err) {
      setError(err instanceof ApiError && err.message ? err.message : msgid('Something went wrong. Try again.'))
    } finally {
      setBusy(false)
    }
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (step === 'username') goTo('password')
    else if (step === 'password') void send({ username: username.trim(), password })
    else void send({ twofa: token })
  }

  const title =
    step === 'username'
      ? t('Welcome to %s', bootstrap?.appName ?? 'Hostisan')
      : step === 'password'
        ? t('Welcome %s!', username)
        : t('Two-factor authentication')
  const description =
    step === 'username'
      ? t('Log in to manage your websites, mail and databases.')
      : step === 'password'
        ? t('Enter your password to continue.')
        : t('Enter the 6-digit token from your authenticator app.')

  return (
    <AuthLayout title={title} description={description}>
      <form onSubmit={submit} className="grid gap-4">
        {(error || signedOutReason) && (
          <Banner variant="error" icon={<WarningCircleIcon weight="fill" />}>
            {t(error || signedOutReason)}
          </Banner>
        )}

        {step === 'username' && (
          <Input
            label={t('Username')}
            name="user"
            autoComplete="username"
            autoFocus
            required
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        )}

        {step !== 'username' && <UsernameChip username={username} onChange={restart} />}

        {step === 'password' && (
          <div className="grid gap-1.5">
            <div className="flex items-center justify-between gap-2">
              <label id="password-label" htmlFor="password" className="text-sm font-medium text-kumo-default">
                {t('Password')}
              </label>
              {canReset && (
                <Link href="/reset" className="text-sm">
                  {t('Forgot your password?')}
                </Link>
              )}
            </div>
            <PasswordInput
              id="password"
              label={null}
              name="password"
              isNew={false}
              autoFocus
              required
              value={password}
              onChange={setPassword}
            />
          </div>
        )}

        {step === 'twofa' && (
          <div className="grid gap-1.5">
            <div className="flex items-center justify-between gap-2">
              <label htmlFor="twofa" className="text-sm font-medium text-kumo-default">
                {t('2FA token')}
              </label>
              <Link href="/reset-2fa" className="text-sm">
                {t('Forgot token?')}
              </Link>
            </div>
            <Input
              id="twofa"
              name="twofa"
              inputMode="numeric"
              autoComplete="one-time-code"
              placeholder="123456"
              maxLength={6}
              autoFocus
              required
              className="font-mono tabular-nums"
              value={token}
              onChange={(e) => setToken(e.target.value.replace(/\D/g, ''))}
            />
          </div>
        )}

        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" variant="primary" icon={SignInIcon} loading={busy}>
            {step === 'username' ? t('Next') : t('Log in')}
          </Button>
          {step !== 'username' && (
            <Button type="button" variant="ghost" icon={ArrowLeftIcon} onClick={restart}>
              {t('Back')}
            </Button>
          )}
          {step === 'username' && canReset && (
            <Link href="/reset" className="ml-auto text-sm">
              {t('Forgot your password?')}
            </Link>
          )}
        </div>

      </form>
    </AuthLayout>
  )
}

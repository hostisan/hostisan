import { InputGroup, Label, Text } from '@cloudflare/kumo'
import {
  CheckCircleIcon,
  CheckIcon,
  CircleIcon,
  CopyIcon,
  EyeIcon,
  EyeSlashIcon,
  SparkleIcon,
} from '@phosphor-icons/react'
import { useId, useState, type ReactNode } from 'react'
import { useT } from '../i18n/I18nProvider'
import { msgid } from '../i18n/msgid'

// Hestia's password rules (web/templates/includes/password-requirements.php).
const RULES = [
  { label: msgid('8 characters long'), test: (p: string) => p.length >= 8 },
  { label: msgid('1 uppercase & 1 lowercase character'), test: (p: string) => /[a-z]/.test(p) && /[A-Z]/.test(p) },
  { label: msgid('1 number'), test: (p: string) => /\d/.test(p) },
]

const STRENGTH = [
  { label: msgid('Too weak'), bar: 'bg-kumo-danger' },
  { label: msgid('Weak'), bar: 'bg-kumo-danger' },
  { label: msgid('Fair'), bar: 'bg-kumo-warning' },
  { label: msgid('Good'), bar: 'bg-kumo-success' },
  { label: msgid('Strong'), bar: 'bg-kumo-success' },
]

// No look-alike characters (0/O, 1/l/I) so generated passwords can be read out or typed.
const LOWER = 'abcdefghijkmnpqrstuvwxyz'
const UPPER = 'ABCDEFGHJKLMNPQRSTUVWXYZ'
const DIGITS = '23456789'
const SYMBOLS = '!@#%^*-_=+'

function pick(chars: string) {
  const n = new Uint32Array(1)
  crypto.getRandomValues(n)
  return chars[n[0] % chars.length]
}

/** 16 characters that always satisfy Hestia's rules, from a CSPRNG. */
export function generatePassword(length = 16) {
  const all = LOWER + UPPER + DIGITS + SYMBOLS
  const chars = [pick(LOWER), pick(UPPER), pick(DIGITS), pick(SYMBOLS)]
  while (chars.length < length) chars.push(pick(all))
  for (let i = chars.length - 1; i > 0; i--) {
    const j = crypto.getRandomValues(new Uint32Array(1))[0] % (i + 1)
    ;[chars[i], chars[j]] = [chars[j], chars[i]]
  }
  return chars.join('')
}

function strengthOf(p: string) {
  if (!p) return 0
  return RULES.filter((r) => r.test(p)).length + (p.length >= 14 && /[^A-Za-z0-9]/.test(p) ? 1 : 0)
}

type PasswordInputProps = {
  /**
   * Pass null when the page renders its own label: set `id`, point the label's htmlFor at
   * it and give the label the id `${id}-label`, which names the field.
   */
  label?: ReactNode | null
  id?: string
  /** Called with each generated password, e.g. to fill a "Confirm password" field too. */
  onGenerate?: (password: string) => void
  autoFocus?: boolean
  description?: ReactNode
  value?: string
  defaultValue?: string
  onChange?: (value: string) => void
  name?: string
  required?: boolean
  disabled?: boolean
  placeholder?: string
  /**
   * New passwords Hostisan sets (accounts, databases, FTP…): generate button, strength
   * bar and Hestia's rules. Turn off for signing in and for credentials that belong to
   * another service (SMTP relay, remote backup host), where generating makes no sense.
   */
  isNew?: boolean
  autoComplete?: string
}

/**
 * The one password field used everywhere: show/hide, copy, and for new passwords a
 * generator, a live strength bar and Hestia's rules checklist.
 */
export function PasswordInput({
  label: labelProp,
  id: idProp,
  onGenerate,
  autoFocus,
  description,
  value: controlled,
  defaultValue = '',
  onChange,
  name,
  required,
  disabled,
  placeholder,
  isNew = true,
  autoComplete,
}: PasswordInputProps) {
  const t = useT()
  const label = labelProp === undefined ? t('Password') : labelProp
  const autoId = useId()
  const id = idProp ?? autoId
  const labelId = `${id}-label`
  const [own, setOwn] = useState(defaultValue)
  const value = controlled ?? own
  const [visible, setVisible] = useState(false)
  const [copied, setCopied] = useState(false)
  const [justGenerated, setJustGenerated] = useState(false)

  const set = (v: string) => {
    if (controlled === undefined) setOwn(v)
    onChange?.(v)
  }
  const generate = () => {
    const pw = generatePassword()
    set(pw)
    onGenerate?.(pw)
    setVisible(true)
    setJustGenerated(true)
  }
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // Clipboard blocked: the password is visible and can be copied by hand.
    }
  }

  const strength = strengthOf(value)
  const hint = justGenerated
    ? t('New password generated. Copy it now; it won’t be shown again after saving.')
    : description

  return (
    <div className="grid gap-1.5">
      {label !== null && (
        <Label htmlFor={id} showOptional={required === false}>
          <span id={labelId}>{label}</span>
        </Label>
      )}
      <InputGroup size="base" disabled={disabled}>
        <InputGroup.Input
          id={id}
          aria-labelledby={labelId}
          name={name}
          type={visible ? 'text' : 'password'}
          value={value}
          onChange={(e) => {
            set(e.target.value)
            setJustGenerated(false)
          }}
          required={required}
          placeholder={placeholder}
          autoFocus={autoFocus}
          autoComplete={autoComplete ?? (isNew ? 'new-password' : 'current-password')}
          spellCheck={false}
          className="font-mono"
        />
        <InputGroup.Addon align="end" className="gap-0.5 pr-1">
          {value && (
            <InputGroup.Button
              variant="ghost"
              tooltip={copied ? t('Copied') : t('Copy')}
              aria-label={t('Copy password')}
              onClick={copy}
              disabled={disabled}
            >
              {copied ? <CheckIcon size={18} className="text-kumo-success" /> : <CopyIcon size={18} />}
            </InputGroup.Button>
          )}
          <InputGroup.Button
            variant="ghost"
            tooltip={visible ? t('Hide password') : t('Show password')}
            aria-label={visible ? t('Hide password') : t('Show password')}
            aria-pressed={visible}
            onClick={() => setVisible((v) => !v)}
            disabled={disabled}
          >
            {visible ? <EyeSlashIcon size={18} /> : <EyeIcon size={18} />}
          </InputGroup.Button>
          {isNew && (
            <InputGroup.Button
              variant="ghost"
              tooltip={t('Generate a strong password')}
              aria-label={t('Generate password')}
              onClick={generate}
              disabled={disabled}
            >
              <SparkleIcon size={18} className="text-kumo-brand" />
            </InputGroup.Button>
          )}
        </InputGroup.Addon>
      </InputGroup>
      {hint && (
        <Text variant={justGenerated ? 'success' : 'secondary'} size="sm">
          {hint}
        </Text>
      )}

      {isNew && !disabled && (
        <div className="grid gap-2 pt-1">
          <div className="flex items-center gap-3">
            <div className="grid flex-1 grid-cols-4 gap-1" aria-hidden>
              {[1, 2, 3, 4].map((step) => (
                <span
                  key={step}
                  className={`h-1 rounded-full ${strength >= step ? STRENGTH[strength].bar : 'bg-kumo-fill'}`}
                />
              ))}
            </div>
            <Text variant="secondary" size="sm" as="span">
              <span className="sr-only">{t('Password strength:')} </span>
              {value ? t(STRENGTH[strength].label) : t('Strength')}
            </Text>
          </div>
          <ul className="grid gap-1" aria-label={t('Password requirements')}>
            {RULES.map((r) => {
              const ok = r.test(value)
              return (
                <li key={r.label} className="flex items-center gap-2">
                  <span className="flex h-lh items-center">
                    {ok ? (
                      <CheckCircleIcon size={16} weight="fill" className="text-kumo-success" />
                    ) : (
                      <CircleIcon size={16} className="text-kumo-subtle" />
                    )}
                  </span>
                  <Text variant={ok ? 'body' : 'secondary'} size="sm" as="span">
                    {t(r.label)}
                  </Text>
                </li>
              )
            })}
          </ul>
        </div>
      )}
    </div>
  )
}

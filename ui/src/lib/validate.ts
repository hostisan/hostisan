// Input checks shared by the forms. They mirror what Hestia's v-* commands accept, so a
// value that passes here won't be rejected by the server for its format.

const HOSTNAME = /^(?=.{1,253}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/i
const IPV4 = /^((25[0-5]|2[0-4]\d|1?\d?\d)\.){3}(25[0-5]|2[0-4]\d|1?\d?\d)$/

/** A fully qualified host name, e.g. ns1.acme-hosting.net. */
export function isHostname(value: string): boolean {
  return HOSTNAME.test(value)
}

/** Web aliases may also be wildcards: *.acme-bakery.com. */
export function isAliasHostname(value: string): boolean {
  return isHostname(value.startsWith('*.') ? value.slice(2) : value)
}

export function isIpv4(value: string): boolean {
  return IPV4.test(value)
}

export function isIpv6(value: string): boolean {
  if (!value.includes(':') || !/^[0-9a-f:.]+$/i.test(value)) return false
  try {
    // The URL parser is a complete IPv6 validator (compression, embedded IPv4).
    new URL(`http://[${value}]`)
    return true
  } catch {
    return false
  }
}

export function isIpAddress(value: string): boolean {
  return isIpv4(value) || isIpv6(value)
}

/** File extensions for the NGINX proxy list: letters and digits only. */
export function isExtension(value: string): boolean {
  return /^[a-z0-9]{1,10}$/i.test(value)
}

/** The part before @ in a mail address, e.g. info or sales.team. */
export function isEmailLocalPart(value: string): boolean {
  return /^[a-z0-9._+-]+$/i.test(value) && !value.startsWith('.') && !value.endsWith('.')
}

export function isEmail(value: string): boolean {
  const at = value.lastIndexOf('@')
  return at > 0 && isEmailLocalPart(value.slice(0, at)) && isHostname(value.slice(at + 1))
}

import { describe, expect, it } from 'vitest'
import { ApiError } from './client'
import { apiErrorText, argMsgids, failureText, retryQuery } from './errors'

const t = (s: string) => ({ Domain: 'Domaine', 'Package %s is used by %s.': 'Le paquet %s sert à %s.' })[s] ?? s

describe('apiErrorText', () => {
  it('fills several %s in order when the counts match', () => {
    const err = new ApiError(400, 'invalid', 'Package %s is used by %s.', ['gold', 'bob'])
    expect(apiErrorText(err, t)).toBe('Le paquet gold sert à bob.')
  })
  it('joins the args into the one %s otherwise', () => {
    const err = new ApiError(400, 'invalid', 'Field "%s" can not be blank.', ['Domain', 'IP Address'])
    expect(apiErrorText(err, t)).toBe('Field "Domaine, IP Address" can not be blank.')
  })
  it('explains a 403 that has no message', () => {
    expect(apiErrorText(new ApiError(403, 'forbidden', ''), t)).toBe("You don't have permission to view this page.")
  })
})

describe('retryQuery', () => {
  it('retries once, but never a 403', () => {
    expect(retryQuery(0, new ApiError(500, 'server_error', ''))).toBe(true)
    expect(retryQuery(1, new ApiError(500, 'server_error', ''))).toBe(false)
    expect(retryQuery(0, new ApiError(403, 'forbidden', ''))).toBe(false)
  })
})

describe('failureText', () => {
  it('names the item and fills its msgid', () => {
    expect(failureText({ name: 'gold', message: 'Package %s is used by %s.', args: ['gold', 'bob'] }, t)).toBe(
      'gold: Le paquet gold sert à bob.',
    )
    expect(failureText({ name: 'x', message: "Error: x doesn't exist" }, t)).toBe("x: Error: x doesn't exist")
  })
})

describe('messageText args', () => {
  const tr = (s: string) =>
    ({
      Domain: 'Domaine',
      Action: 'Action FR',
      'Field "%s" can not be blank.': 'Le champ "%s" est obligatoire.',
      'Field "%s" is invalid.': 'Le champ "%s" est invalide.',
      'Invalid port: %s': 'Port invalide : %s',
    })[s] ?? s

  it('errors.ts translates args only for blank-field messages', () => {
    expect(apiErrorText(new ApiError(400, 'invalid', 'Field "%s" can not be blank.', ['Domain']), tr)).toBe(
      'Le champ "Domaine" est obligatoire.',
    )
    expect(apiErrorText(new ApiError(400, 'invalid', 'Field "%s" is invalid.', ['Action']), tr)).toBe(
      'Le champ "Action FR" est invalide.',
    )
    // A user-typed value is shown as typed, even when it happens to be a msgid.
    expect(apiErrorText(new ApiError(400, 'invalid', 'Invalid port: %s', ['Domain']), tr)).toBe('Port invalide : Domain')
  })

  it('lists the label msgids', () => {
    expect(argMsgids).toEqual(['Field "%s" can not be blank.', 'Field "%s" is invalid.'])
  })
})

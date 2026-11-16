import { describe, expect, it } from 'vitest'
import { isTypingTarget, matches } from './keys'

function key(init: KeyboardEventInit) {
  return new KeyboardEvent('keydown', init)
}

describe('matches', () => {
  it('matches needs the exact modifiers', () => {
    expect(matches(key({ key: 'a' }), { key: 'a' }, false)).toBe(true)
    expect(matches(key({ key: 'A', shiftKey: true }), { key: 'a' }, false)).toBe(false)
    expect(
      matches(key({ key: 'A', shiftKey: true, ctrlKey: true }), { key: 'a', ctrl: true, shift: true }, false),
    ).toBe(true)
    expect(matches(key({ key: 'a', ctrlKey: true }), { key: 'a' }, false)).toBe(false)
    expect(matches(key({ key: 'a', altKey: true }), { key: 'a' }, false)).toBe(false)
    expect(matches(key({ key: 'a', metaKey: true }), { key: 'a' }, false)).toBe(false)
    expect(matches(key({ key: 'Enter' }), { key: 'Enter', ctrl: true }, false)).toBe(false)
    expect(matches(key({ key: '?', shiftKey: true }), { key: '?', shift: true }, false)).toBe(true)
    expect(matches(key({ key: 'x', code: 'KeyA' }), { code: 'KeyA' }, false)).toBe(true)
  })

  it('ctrl matches meta on mac', () => {
    expect(matches(key({ key: 'k', metaKey: true }), { key: 'k', ctrl: true }, true)).toBe(true)
    expect(matches(key({ key: 'k', ctrlKey: true }), { key: 'k', ctrl: true }, true)).toBe(true)
    expect(matches(key({ key: 'k', metaKey: true }), { key: 'k', ctrl: true }, false)).toBe(false)
    expect(matches(key({ key: 'k', metaKey: true }), { key: 'k' }, true)).toBe(false)
  })
})

describe('isTypingTarget', () => {
  function el(html: string) {
    document.body.innerHTML = html
    return document.body.querySelector('[data-t]')
  }

  it('isTypingTarget covers input textarea select contenteditable and combobox roles', () => {
    expect(isTypingTarget(el('<input data-t>'))).toBe(true)
    expect(isTypingTarget(el('<input type="search" data-t>'))).toBe(true)
    expect(isTypingTarget(el('<textarea data-t></textarea>'))).toBe(true)
    expect(isTypingTarget(el('<select data-t></select>'))).toBe(true)
    expect(isTypingTarget(el('<div contenteditable="true"><p data-t>x</p></div>'))).toBe(true)
    expect(isTypingTarget(el('<div contenteditable="false" data-t>x</div>'))).toBe(false)
    for (const role of ['combobox', 'listbox', 'menu', 'menuitem', 'textbox']) {
      expect(isTypingTarget(el(`<div role="${role}"><span data-t>x</span></div>`))).toBe(true)
    }
    expect(isTypingTarget(el('<input type="checkbox" data-t>'))).toBe(false)
    expect(isTypingTarget(el('<button data-t>x</button>'))).toBe(false)
    expect(isTypingTarget(el('<a href="/x" data-t>x</a>'))).toBe(false)
    expect(isTypingTarget(document.body)).toBe(false)
    expect(isTypingTarget(null)).toBe(false)
    expect(isTypingTarget(window)).toBe(false)
  })
})

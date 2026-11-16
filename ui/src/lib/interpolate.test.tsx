import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { interpolate } from './interpolate'

function html(node: React.ReactNode) {
  return render(<p>{node}</p>).container.firstElementChild!.innerHTML
}

describe('interpolate', () => {
  it('fills each %s with its node, in order', () => {
    expect(html(interpolate('Files in %s belong to %s.', <code>/home</code>, <b>bob</b>))).toBe(
      'Files in <code>/home</code> belong to <b>bob</b>.',
    )
  })

  it('works on a translated sentence whose placeholders moved', () => {
    expect(html(interpolate('%s : supprimer %s', <i>x</i>, 'y'))).toBe('<i>x</i> : supprimer y')
  })

  it('returns the text unchanged when the counts differ', () => {
    expect(interpolate('Delete %s and %s', <b>a</b>)).toBe('Delete %s and %s')
    expect(interpolate('No placeholder', <b>a</b>)).toBe('No placeholder')
  })

  it('returns plain text when there is nothing to fill', () => {
    expect(html(interpolate('Just text'))).toBe('Just text')
  })
})

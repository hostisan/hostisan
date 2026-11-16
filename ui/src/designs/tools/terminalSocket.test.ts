import { describe, expect, it, vi } from 'vitest'
import { openTerminal } from './terminalSocket'

class FakeSocket {
  static last: FakeSocket
  sent: (string | ArrayBufferLike | ArrayBufferView)[] = []
  binaryType = ''
  closed = false
  onopen: (() => void) | null = null
  onmessage: ((e: { data: unknown }) => void) | null = null
  onclose: ((e: { code: number; reason: string; wasClean: boolean }) => void) | null = null
  onerror: (() => void) | null = null
  url: string
  constructor(url: string) {
    this.url = url
    FakeSocket.last = this
  }
  send(d: string | ArrayBufferLike | ArrayBufferView) {
    this.sent.push(d)
  }
  close() {
    this.closed = true
  }
}

function open() {
  const onData = vi.fn()
  const onClose = vi.fn()
  const term = openTerminal({
    url: 'wss://panel.test/api/v1/terminal/socket',
    token: 't',
    cols: 80,
    rows: 24,
    onData,
    onClose,
    WebSocketImpl: FakeSocket as unknown as typeof WebSocket,
  })
  return { ws: FakeSocket.last, term, onData, onClose }
}

describe('terminal socket', () => {
  it('sends the auth message first, then binary input and JSON resizes', () => {
    const { ws, term } = open()
    expect(ws.url).toBe('wss://panel.test/api/v1/terminal/socket')
    expect(ws.binaryType).toBe('arraybuffer')
    ws.onopen?.()
    expect(ws.sent[0]).toBe('{"type":"auth","token":"t","cols":80,"rows":24}')
    term.send('ls')
    expect(typeof ws.sent[1]).not.toBe('string')
    expect(new TextDecoder().decode(ws.sent[1] as Uint8Array)).toBe('ls')
    term.resize(100, 30)
    expect(ws.sent[2]).toBe('{"type":"resize","cols":100,"rows":30}')
  })

  it('drops input and resizes until the socket is open, and after it ends', () => {
    const { ws, term } = open()
    term.send('early')
    term.resize(90, 20)
    expect(ws.sent).toEqual([])
    ws.onopen?.()
    expect(ws.sent).toHaveLength(1)
    ws.onclose?.({ code: 1000, reason: '', wasClean: true })
    term.send('late')
    term.resize(90, 20)
    expect(ws.sent).toHaveLength(1)
  })

  it('passes server output through as bytes', () => {
    const { ws, onData } = open()
    ws.onmessage?.({ data: new Uint8Array([104, 105]).buffer })
    expect(Array.from(onData.mock.calls[0][0] as Uint8Array)).toEqual([104, 105])
  })

  it('reports a clean close with its reason', () => {
    const { ws, onClose } = open()
    ws.onclose?.({ code: 1000, reason: 'Your session has expired.', wasClean: true })
    expect(onClose).toHaveBeenCalledWith('Your session has expired.', true)
  })

  it('names a clean close without a reason', () => {
    const { ws, onClose } = open()
    ws.onclose?.({ code: 1000, reason: '', wasClean: true })
    expect(onClose).toHaveBeenCalledWith('Connection closed.', true)
  })

  it('reports an error once, and not again on the close that follows', () => {
    const { ws, onClose } = open()
    ws.onerror?.()
    ws.onclose?.({ code: 1006, reason: '', wasClean: false })
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(onClose).toHaveBeenCalledWith('Connection error.', false)
  })

  it('close() shuts the socket without reporting', () => {
    const { ws, term, onClose } = open()
    term.close()
    expect(ws.closed).toBe(true)
    ws.onclose?.({ code: 1000, reason: '', wasClean: true })
    expect(onClose).not.toHaveBeenCalled()
  })
})

// The browser side of the terminal protocol: the first message authenticates with the
// single-use token; after that, binary frames carry keystrokes and output, and a JSON
// text message resizes the pty.

import { msgid } from '../../i18n/msgid'

export type TerminalOptions = {
  url: string
  token: string
  cols: number
  rows: number
  onData(d: Uint8Array): void
  /** reason is an English msgid when the socket (not the server) ended it. */
  onClose(reason: string, clean: boolean): void
  /** For tests. */
  WebSocketImpl?: typeof WebSocket
}

export type TerminalConnection = {
  send(d: string): void
  resize(cols: number, rows: number): void
  close(): void
}

export function openTerminal(o: TerminalOptions): TerminalConnection {
  const Impl = o.WebSocketImpl ?? WebSocket
  const ws = new Impl(o.url)
  ws.binaryType = 'arraybuffer'
  const enc = new TextEncoder()
  let finished = false
  let opened = false
  // A socket still connecting throws on send; input before open or after the end is dropped.
  const sendIfOpen = (d: string | Uint8Array<ArrayBuffer>) => {
    if (opened && !finished) ws.send(d)
  }
  const finish = (reason: string, clean: boolean) => {
    if (finished) return
    finished = true
    o.onClose(reason, clean)
  }

  ws.onopen = () => {
    opened = true
    ws.send(JSON.stringify({ type: 'auth', token: o.token, cols: o.cols, rows: o.rows }))
  }
  ws.onmessage = (e) => {
    if (e.data instanceof ArrayBuffer) o.onData(new Uint8Array(e.data))
    else if (typeof e.data === 'string') o.onData(enc.encode(e.data))
  }
  ws.onerror = () => finish(msgid('Connection error.'), false)
  ws.onclose = (e) => finish(e.reason || (e.wasClean ? msgid('Connection closed.') : msgid('Connection error.')), e.wasClean)

  return {
    send: (d) => sendIfOpen(enc.encode(d)),
    resize: (cols, rows) => sendIfOpen(JSON.stringify({ type: 'resize', cols, rows })),
    close: () => {
      finished = true
      ws.close()
    },
  }
}

import { Button, Table } from '@cloudflare/kumo'
import { cleanup, fireEvent, screen, waitFor } from '@testing-library/react'
import { useState, type ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { TableCard } from '../components/table'
import { AppLayout } from '../layout/AppLayout'
import { renderApp, signedIn } from '../test/renderApp'
import { useRowShortcuts } from './useRowShortcuts'

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const done: string[] = []

/** A list page in the shell: Enter opens, s suspends, Delete asks first; "b" has no s. */
function RowsProbe({ initial = ['a', 'b', 'c'] }: { initial?: string[] }) {
  const [rows, setRows] = useState(initial)
  const [asking, setAsking] = useState<string | null>(null)
  const keys = useRowShortcuts(
    rows,
    (r) => r,
    (r) => ({
      enter: () => done.push(`open ${r}`),
      delete: () => setAsking(r),
      ...(r === 'b' ? {} : { s: () => done.push(`suspend ${r}`) }),
    }),
  )
  return (
    <div>
      <Button onClick={() => setRows((rs) => rs.slice(1))}>Drop first</Button>
      <input aria-label="Filter" />
      <TableCard
        filteredCount={rows.length}
        totalCount={rows.length}
        noun="rows"
        emptyState={empty}
        rowShortcuts={keys}
      >
        <Table>
          <Table.Body>
            {rows.map((r) => (
              <Table.Row key={r} {...keys.rowProps(r)}>
                <Table.Cell>row {r}</Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </Table>
      </TableCard>
      <ConfirmDialog
        open={asking !== null}
        onOpenChange={(o) => !o && setAsking(null)}
        title="Delete row"
        description="Are you sure?"
        confirmLabel="Delete"
        destructive
        onConfirm={() => done.push(`delete ${asking}`)}
      />
    </div>
  )
}

const empty = { icon: null, title: 'None' }

function inShell(element: ReactNode) {
  return [{ path: '/probe', element: <AppLayout />, children: [{ index: true, element }] }]
}

async function probe(element: ReactNode = <RowsProbe />) {
  done.length = 0
  renderApp('/probe', { 'GET /bootstrap': () => ({ body: signedIn({}) }) }, inShell(element))
  await screen.findByRole('button', { name: 'admin' })
  await screen.findByText('row a')
}

const focused = () => document.querySelector('[data-row-focused]')?.textContent ?? null
const press = (key: string, init: KeyboardEventInit = {}) => fireEvent.keyDown(document.body, { key, ...init })

describe('row shortcuts', () => {
  it('arrow down focuses the first row then the next', async () => {
    await probe()
    expect(focused()).toBeNull()
    expect(press('ArrowDown')).toBe(false)
    expect(focused()).toBe('row a')
    press('ArrowDown')
    expect(focused()).toBe('row b')
    press('ArrowDown')
    expect(focused()).toBe('row c')
    // At the end the key goes back to the page, which scrolls.
    expect(press('ArrowDown')).toBe(true)
    expect(focused()).toBe('row c')
  })

  it('arrow up stops at the first row', async () => {
    await probe()
    expect(press('ArrowUp')).toBe(true)
    expect(focused()).toBeNull()
    press('ArrowDown')
    press('ArrowDown')
    expect(press('ArrowUp')).toBe(false)
    expect(focused()).toBe('row a')
    expect(press('ArrowUp')).toBe(true)
    expect(focused()).toBe('row a')
  })

  it("enter runs the row's enter action", async () => {
    await probe()
    expect(press('Enter')).toBe(true)
    expect(done).toEqual([])
    press('ArrowDown')
    press('ArrowDown')
    expect(press('Enter')).toBe(false)
    expect(done).toEqual(['open b'])
  })

  it('delete runs the delete action, which asks first', async () => {
    await probe()
    press('ArrowDown')
    expect(press('Delete')).toBe(false)
    await screen.findByRole('dialog')
    expect(done).toEqual([])
    // Keys wait while the confirm is open.
    expect(press('Enter')).toBe(true)
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(done).toEqual(['delete a'])
  })

  it('a letter without an action does nothing', async () => {
    await probe()
    press('ArrowDown')
    press('ArrowDown')
    expect(press('s')).toBe(true)
    expect(press('w')).toBe(true)
    press('ArrowDown')
    expect(press('s')).toBe(false)
    expect(press('S', { shiftKey: true })).toBe(true)
    expect(done).toEqual(['suspend c'])
  })

  it('keys typed in a field stay in the field', async () => {
    await probe()
    press('ArrowDown')
    const field = screen.getByRole('textbox', { name: 'Filter' })
    field.focus()
    expect(fireEvent.keyDown(field, { key: 'ArrowDown' })).toBe(true)
    expect(fireEvent.keyDown(field, { key: 's' })).toBe(true)
    expect(focused()).toBe('row a')
    expect(done).toEqual([])
  })

  it('focus resets when the rows change', async () => {
    await probe()
    press('ArrowDown')
    press('ArrowDown')
    expect(focused()).toBe('row b')
    fireEvent.click(screen.getByRole('button', { name: 'Drop first' }))
    await waitFor(() => expect(screen.queryByText('row a')).toBeNull())
    expect(focused()).toBeNull()
    press('ArrowDown')
    expect(focused()).toBe('row b')
  })
})

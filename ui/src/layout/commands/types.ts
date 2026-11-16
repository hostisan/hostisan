import type { Icon } from '@phosphor-icons/react'

export type CommandItem = { id: string; title: string; hint?: string; icon: Icon; href: string; keywords?: string }
export type CommandGroup = { id: string; label: string; items: CommandItem[] }
/** What one section adds to the ⌘K palette. */
export type CommandContribution = { actions: CommandItem[]; pages: CommandItem[]; groups: CommandGroup[] }
export const noCommands: CommandContribution = { actions: [], pages: [], groups: [] }

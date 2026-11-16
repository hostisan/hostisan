import ts from 'typescript'
import { expect } from 'vitest'
import { allowUntranslated } from '../i18n/allowUntranslated'

/**
 * Finds UI text that doesn't go through t(): JSX text, literal translatable
 * attributes, literal labels in objects, and t() calls whose msgid is built at run
 * time. The TypeScript parser reads the source; nothing is run.
 */

export type Finding = {
  file: string
  line: number
  text: string
  kind: 'jsx-text' | 'jsx-attr' | 'object-key' | 't-template' | 't-concat'
}

/** JSX attributes whose string value is shown to people (or read out by a screen reader). */
const ATTRS = new Set([
  'label',
  'noun',
  'resourceType',
  'title',
  'description',
  'placeholder',
  'message',
  'confirmLabel',
  'submitLabel',
  'successMessage',
  'aria-label',
  'alt',
])

/** Object keys whose string value ends up on screen. */
const KEYS = new Set(['label', 'title', 'description', 'placeholder', 'message', 'confirmLabel', 'submitLabel', 'successMessage'])

const LETTER = /\p{L}/u

function shown(text: string): boolean {
  const s = text.trim()
  return LETTER.test(s) && !allowUntranslated.has(s)
}

/** The literal strings an expression can evaluate to, through ?:, ||, ??, () and object values. */
function literals(e: ts.Expression): ts.Expression[] {
  if (ts.isParenthesizedExpression(e)) return literals(e.expression)
  if (ts.isStringLiteral(e) || ts.isNoSubstitutionTemplateLiteral(e)) return [e]
  // A { one, other } noun pair.
  if (ts.isObjectLiteralExpression(e))
    return e.properties.flatMap((p) => (ts.isPropertyAssignment(p) ? literals(p.initializer) : []))
  if (ts.isConditionalExpression(e)) return [...literals(e.whenTrue), ...literals(e.whenFalse)]
  if (
    ts.isBinaryExpression(e) &&
    (e.operatorToken.kind === ts.SyntaxKind.BarBarToken || e.operatorToken.kind === ts.SyntaxKind.QuestionQuestionToken)
  )
    return [...literals(e.left), ...literals(e.right)]
  return []
}

function isTCall(n: ts.CallExpression): boolean {
  const c = n.expression
  return (ts.isIdentifier(c) && c.text === 't') || (ts.isPropertyAccessExpression(c) && c.name.text === 't')
}

function propName(n: ts.PropertyName): string | undefined {
  if (ts.isIdentifier(n) || ts.isStringLiteral(n)) return n.text
  return undefined
}

function auditSource(file: string, source: string): Finding[] {
  const sf = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, file.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
  const out: Finding[] = []
  const add = (node: ts.Node, kind: Finding['kind'], text: string) =>
    out.push({ file, line: sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1, text, kind })
  const addLiterals = (e: ts.Expression, kind: Finding['kind']) => {
    for (const lit of literals(e)) {
      const text = (lit as ts.StringLiteral).text
      if (shown(text)) add(lit, kind, text.trim())
    }
  }

  const visit = (n: ts.Node) => {
    if (ts.isJsxText(n)) {
      if (shown(n.text)) add(n, 'jsx-text', n.text.trim().replace(/\s+/g, ' '))
    } else if (ts.isJsxExpression(n) && n.expression && (ts.isJsxElement(n.parent) || ts.isJsxFragment(n.parent))) {
      addLiterals(n.expression, 'jsx-text')
    } else if (ts.isJsxAttribute(n)) {
      const name = ts.isIdentifier(n.name) ? n.name.text : n.name.getText(sf)
      if (ATTRS.has(name) && n.initializer) {
        if (ts.isStringLiteral(n.initializer)) addLiterals(n.initializer, 'jsx-attr')
        else if (ts.isJsxExpression(n.initializer) && n.initializer.expression)
          addLiterals(n.initializer.expression, 'jsx-attr')
      }
    } else if (ts.isPropertyAssignment(n)) {
      const name = propName(n.name)
      if (name && KEYS.has(name)) addLiterals(n.initializer, 'object-key')
    } else if (ts.isCallExpression(n) && isTCall(n) && n.arguments.length > 0) {
      const arg = n.arguments[0]
      if (ts.isTemplateExpression(arg)) add(arg, 't-template', arg.getText(sf))
      else if (ts.isBinaryExpression(arg) && arg.operatorToken.kind === ts.SyntaxKind.PlusToken)
        add(arg, 't-concat', arg.getText(sf))
    }
    ts.forEachChild(n, visit)
  }
  visit(sf)
  return out
}

/** Audits in-memory sources, keyed by file name (a .tsx name parses JSX). */
export function findUntranslatedIn(sources: Record<string, string>): Finding[] {
  return Object.entries(sources).flatMap(([file, source]) => auditSource(file, source))
}

// Every UI source file except tests and generated API types, keyed "src/…".
const tree = Object.fromEntries(
  Object.entries(
    import.meta.glob(['../**/*.{ts,tsx}', '!../**/*.test.{ts,tsx}', '!../**/*.gen.ts', '!../test/**'], {
      query: '?raw',
      import: 'default',
      eager: true,
    }) as Record<string, string>,
  ).map(([path, source]) => [`src/${path.slice(3)}`, source]),
)

function pick(dirs: string[]): Record<string, string> {
  // "src", "src/" and "./src" all mean the whole tree; "designs/dns" is under src/.
  const prefixes = dirs.map((d) =>
    `src/${d
      .replace(/^(\.\/)?(src(\/|$))?/, '')
      .replace(/\/+$/, '')}/`.replace(/\/\/$/, '/'),
  )
  return Object.fromEntries(Object.entries(tree).filter(([file]) => prefixes.some((p) => file.startsWith(p))))
}

/** The files ("src/…") the audit reads for the given folders. */
export function auditedFiles(dirs: string[]): string[] {
  return Object.keys(pick(dirs)).sort()
}

/** Audits the source folders given relative to ui/ ("src/designs/dns") or src/ ("designs/dns"). */
export function findUntranslated(dirs: string[]): Finding[] {
  return findUntranslatedIn(pick(dirs))
}

/** Fails the test with one line per finding in the given folders. */
export function expectTranslated(...dirs: string[]): void {
  const lines = findUntranslated(dirs).map((f) => `${f.file}:${f.line} ${f.kind} ${JSON.stringify(f.text)}`)
  expect(lines, 'UI text that does not go through t()').toEqual([])
}

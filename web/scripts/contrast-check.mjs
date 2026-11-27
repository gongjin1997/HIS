#!/usr/bin/env node
// DESIGN.md v3.0: WCAG 2.2 automated contrast checker parsing web/src/styles/tokens.css via PostCSS
import fs from 'node:fs'
import path from 'node:path'
import postcss from 'postcss'

const tokensPath = path.resolve('src/styles/tokens.css')
const root = postcss.parse(fs.readFileSync(tokensPath, 'utf8'))

const rawMaps = { base: {}, light: {}, dark: {} }
root.walkRules((rule) => {
  if (rule.parent && rule.parent.type === 'atrule') return
  const sel = rule.selector.replace(/\s+/g, ' ').trim()
  const target =
    sel === ':root'
      ? 'base'
      : sel.includes('[data-theme="light"]')
        ? 'light'
        : sel.includes('[data-theme="dark"]')
          ? 'dark'
          : null
  if (!target) return
  rule.walkDecls((decl) => {
    if (decl.prop.startsWith('--ki-')) rawMaps[target][decl.prop] = decl.value.trim()
  })
})

function resolveToken(theme, name, depth = 0) {
  if (depth > 12) throw new Error(`cyclic var ${name}`)
  const map =
    theme === 'dark'
      ? { ...rawMaps.base, ...rawMaps.light, ...rawMaps.dark }
      : { ...rawMaps.base, ...rawMaps.light }
  const val = map[name]
  if (!val) throw new Error(`missing token ${name} in theme ${theme}`)
  const m = val.match(/^var\((--ki-[a-z0-9-]+)\)$/)
  if (m) return resolveToken(theme, m[1], depth + 1)
  return val
}

function parseColor(str) {
  if (str.startsWith('#')) {
    const hex = str.slice(1)
    const full = hex.length === 3 ? hex.split('').map((c) => c + c).join('') : hex
    return {
      r: parseInt(full.slice(0, 2), 16),
      g: parseInt(full.slice(2, 4), 16),
      b: parseInt(full.slice(4, 6), 16),
      a: 1,
    }
  }
  const m = str.match(/^rgba?\(\s*([\d.]+)\s*,\s*([\d.]+)\s*,\s*([\d.]+)(?:\s*,\s*([\d.]+))?\s*\)$/)
  if (m) return { r: Number(m[1]), g: Number(m[2]), b: Number(m[3]), a: m[4] === undefined ? 1 : Number(m[4]) }
  throw new Error(`unparseable color: ${str}`)
}

function composite(fg, bg) {
  const a = fg.a
  return {
    r: fg.r * a + bg.r * (1 - a),
    g: fg.g * a + bg.g * (1 - a),
    b: fg.b * a + bg.b * (1 - a),
    a: 1,
  }
}

function luminance(c) {
  const lin = (v) => {
    const s = v / 255
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4)
  }
  return 0.2126 * lin(c.r) + 0.7152 * lin(c.g) + 0.0722 * lin(c.b)
}

function contrastRatio(fg, bg) {
  const f = composite(fg, bg)
  const l1 = luminance(f)
  const l2 = luminance(bg)
  return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05)
}

const checks = [
  // Light (default) text, controls & status badges
  ['light', '--ki-text-primary', '--ki-bg-surface', 7.0],
  ['light', '--ki-text-secondary', '--ki-bg-surface', 4.5],
  ['light', '--ki-text-muted', '--ki-bg-surface', 4.5],
  ['light', '--ki-text-muted', '--ki-bg-subtle', 4.5],
  ['light', '--ki-text-link', '--ki-bg-surface', 4.5],
  ['light', '--ki-border-control', '--ki-bg-surface', 3.0],
  ['light', '--ki-border-focus', '--ki-bg-surface', 3.0],
  ['light', '--ki-text-on-solid', '--ki-brand-solid', 4.5],
  ['light', '--ki-text-on-solid', '--ki-danger-solid', 4.5],
  ['light', '--ki-status-ok-text', '--ki-status-ok-subtle', 4.5],
  ['light', '--ki-status-running-text', '--ki-status-running-subtle', 4.5],
  ['light', '--ki-status-warn-text', '--ki-status-warn-subtle', 4.5],
  ['light', '--ki-status-danger-text', '--ki-status-danger-subtle', 4.5],
  ['light', '--ki-badge-arm64k-text', '--ki-badge-arm64k-bg', 4.5],
  // Dark text, controls & status badges
  ['dark', '--ki-text-primary', '--ki-bg-surface', 7.0],
  ['dark', '--ki-text-secondary', '--ki-bg-surface', 4.5],
  ['dark', '--ki-text-muted', '--ki-bg-surface', 4.5],
  ['dark', '--ki-text-muted', '--ki-bg-subtle', 4.5],
  ['dark', '--ki-text-link', '--ki-bg-surface', 4.5],
  ['dark', '--ki-border-control', '--ki-bg-surface', 3.0],
  ['dark', '--ki-border-focus', '--ki-bg-surface', 3.0],
  ['dark', '--ki-text-on-solid', '--ki-brand-solid', 4.5],
  ['dark', '--ki-text-on-solid', '--ki-danger-solid', 4.5],
  ['dark', '--ki-status-ok-text', '--ki-status-ok-subtle', 4.5, '--ki-bg-surface'],
  ['dark', '--ki-status-running-text', '--ki-status-running-subtle', 4.5, '--ki-bg-surface'],
  ['dark', '--ki-status-warn-text', '--ki-status-warn-subtle', 4.5, '--ki-bg-surface'],
  ['dark', '--ki-status-danger-text', '--ki-status-danger-subtle', 4.5, '--ki-bg-surface'],
  ['dark', '--ki-badge-arm64k-text', '--ki-badge-arm64k-bg', 4.5, '--ki-bg-surface'],
  // Terminal ANSI contrast
  ['dark', '--ki-term-fg', '--ki-bg-terminal', 7.0],
  ['dark', '--ki-ansi-1', '--ki-bg-terminal', 4.5],
  ['dark', '--ki-ansi-2', '--ki-bg-terminal', 4.5],
  ['dark', '--ki-ansi-3', '--ki-bg-terminal', 4.5],
  ['dark', '--ki-ansi-4', '--ki-bg-terminal', 4.5],
]

let failed = 0
for (const [theme, fgTok, bgTok, minRatio, surfaceTok] of checks) {
  const fg = parseColor(resolveToken(theme, fgTok))
  let bg = parseColor(resolveToken(theme, bgTok))
  if (surfaceTok && bg.a < 1) {
    bg = composite(bg, parseColor(resolveToken(theme, surfaceTok)))
  }
  const ratio = contrastRatio(fg, bg)
  if (ratio < minRatio) {
    console.error(`FAIL [${theme}] ${fgTok} on ${bgTok}: ${ratio.toFixed(2)}:1 < ${minRatio}:1`)
    failed++
  }
}

if (failed > 0) {
  console.error(`\nWCAG 2.2 contrast audit failed (${failed} pairs below threshold).`)
  process.exit(1)
}
console.log(`WCAG 2.2 contrast audit PASSED: all ${checks.length} token pairs meet DESIGN.md v3.0 thresholds.`)

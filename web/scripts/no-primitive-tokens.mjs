#!/usr/bin/env node
// DESIGN.md §12.2: forbid direct references to primitive palette tokens (--ki-p-*) outside tokens.css (and whitelisted print block in base.css)
import fs from 'node:fs'
import path from 'node:path'

const srcDir = path.resolve('src')
let violations = 0

function walk(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      walk(full)
      continue
    }
    if (!/\.(css|vue|ts)$/.test(entry.name)) continue
    if (full.endsWith(path.join('styles', 'tokens.css'))) continue
    const lines = fs.readFileSync(full, 'utf8').split('\n')
    lines.forEach((line, idx) => {
      if (line.includes('var(--ki-p-')) {
        const prev = lines[idx - 1] || ''
        if (full.endsWith(path.join('styles', 'base.css')) && prev.includes('stylelint-disable-next-line')) {
          return
        }
        console.error(`PRIMITIVE TOKEN VIOLATION (DESIGN.md §12.5 #12): ${path.relative(process.cwd(), full)}:${idx + 1}: ${line.trim()}`)
        violations++
      }
    })
  }
}

walk(srcDir)
if (violations > 0) {
  console.error(`\nFound ${violations} primitive token violations.`)
  process.exit(1)
}
console.log('Primitive token check passed: 0 violations outside tokens.css.')

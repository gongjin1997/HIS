#!/usr/bin/env node
// DESIGN.md §12.4: export tokens.css one-way into W3C DTCG JSON at ../design/tokens.json
import fs from 'node:fs'
import path from 'node:path'
import postcss from 'postcss'

const tokensPath = path.resolve('src/styles/tokens.css')
const outPath = path.resolve('../design/tokens.json')
const root = postcss.parse(fs.readFileSync(tokensPath, 'utf8'))

const out = {
  $schema: 'https://tr.designtokens.org/format/',
  $description: 'Generated one-way from web/src/styles/tokens.css (kinstall Design System v2.0.0)',
  primitive: {},
  semantic: { base: {}, dark: {}, light: {} },
  density: { compact: {} },
}

root.walkRules((rule) => {
  if (rule.parent && rule.parent.type === 'atrule') return
  const sel = rule.selector.replace(/\s+/g, ' ').trim()
  rule.walkDecls((decl) => {
    if (!decl.prop.startsWith('--ki-')) return
    const type = decl.value.startsWith('#') || decl.value.startsWith('rgb') ? 'color' : decl.value.endsWith('px') ? 'dimension' : 'other'
    const entry = { $value: decl.value.trim(), $type: type }
    if (decl.prop.startsWith('--ki-p-')) {
      out.primitive[decl.prop] = entry
    } else if (sel.includes('[data-density="compact"]')) {
      out.density.compact[decl.prop] = entry
    } else if (sel.includes('[data-theme="light"]')) {
      out.semantic.light[decl.prop] = entry
    } else if (sel.includes('[data-theme="dark"]')) {
      out.semantic.dark[decl.prop] = entry
    } else {
      out.semantic.base[decl.prop] = entry
    }
  })
})

fs.mkdirSync(path.dirname(outPath), { recursive: true })
fs.writeFileSync(outPath, JSON.stringify(out, null, 2) + '\n', 'utf8')
console.log(`Exported W3C DTCG tokens to ${path.relative(process.cwd(), outPath)}`)

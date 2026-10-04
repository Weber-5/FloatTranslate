/* eslint-disable */
// One-off audit: every t('...') literal in src must exist in the zh-CN catalog.
const fs = require('fs')
const path = require('path')

function walk(dir, files = []) {
  for (const entry of fs.readdirSync(dir)) {
    const p = path.join(dir, entry)
    const stat = fs.statSync(p)
    if (stat.isDirectory()) walk(p, files)
    else if (/\.(vue|ts)$/.test(entry) && !entry.endsWith('.d.ts')) files.push(p)
  }
  return files
}

function leaves(obj, prefix = '') {
  const out = []
  for (const [key, value] of Object.entries(obj)) {
    const full = prefix ? prefix + '.' + key : key
    if (value && typeof value === 'object') out.push(...leaves(value, full))
    else out.push(full)
  }
  return out
}

const src = fs.readFileSync(path.join(process.cwd(), 'src/i18n/zh-CN.ts'), 'utf8')
const json = src
  .replace(/^\s*\/\*\*[\s\S]*?\*\/\s*/, '')
  .replace(/export default\s*/, 'module.exports =\n')
  .replace(/} as const\s*$/, '}')
const mod = { exports: {} }
new Function('module', 'exports', json)(mod, mod.exports)
const catalog = new Set(leaves(mod.exports))

const bad = new Set()
for (const file of walk(path.join(process.cwd(), 'src'))) {
  const text = fs.readFileSync(file, 'utf8')
  for (const m of text.matchAll(/\bt\(\s*'([^']+)'/g)) {
    const key = m[1]
    if (key.includes('${')) continue
    if (!catalog.has(key)) bad.add(file + ' -> ' + key)
  }
  for (const m of text.matchAll(/\bt\(\s*`([^`]+)`/g)) {
    const key = m[1]
    if (key.includes('${')) {
      const base = key.split('${')[0]
      let found = false
      for (const entry of catalog) if (entry.startsWith(base)) found = true
      if (!found) bad.add(file + ' -> ' + key)
    } else if (!catalog.has(key)) {
      bad.add(file + ' -> ' + key)
    }
  }
}

if (bad.size === 0) console.log('ALL KEYS OK (' + catalog.size + ' catalog keys)')
else {
  console.log('MISSING KEYS:')
  for (const entry of bad) console.log('  ' + entry)
  process.exitCode = 1
}

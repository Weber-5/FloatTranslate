#!/usr/bin/env node
/**
 * Version consistency check — CI stage 1 (docs/10 §2 Contract).
 *
 * Source of truth: src-tauri/tauri.conf.json `version` (1.0.0). The following
 * must all match it (docs/10 §7: app releases use unified SemVer):
 *
 *   - src-tauri/tauri.conf.json           -> version            (source of truth)
 *   - frontend/src/constants.ts           -> APP_VERSION
 *   - openapi/openapi.yaml                -> info.version
 *   - frontend/package.json               -> version
 *   - src-tauri/Cargo.toml                -> [package] version
 *
 * Exit 0 = all in sync; exit 1 = at least one mismatch.
 */

import { readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..');
const read = (...parts) => readFileSync(join(repoRoot, ...parts), 'utf8');

/** @type {Array<[string, string]>} name -> version */
const found = [];
/** @type {string[]} */
const errors = [];

// Source of truth.
let appVersion;
try {
  appVersion = JSON.parse(read('src-tauri', 'tauri.conf.json')).version;
} catch (err) {
  console.error(`cannot read src-tauri/tauri.conf.json: ${err instanceof Error ? err.message : err}`);
  process.exit(1);
}
if (typeof appVersion !== 'string' || !/^\d+\.\d+\.\d+/.test(appVersion)) {
  console.error(`src-tauri/tauri.conf.json version "${appVersion}" is not valid SemVer`);
  process.exit(1);
}
found.push(['src-tauri/tauri.conf.json (source of truth)', appVersion]);

// frontend/src/constants.ts -> APP_VERSION = '1.0.0'
{
  const m = read('frontend', 'src', 'constants.ts').match(/APP_VERSION\s*=\s*['"]([^'"]+)['"]/);
  if (!m) errors.push('frontend/src/constants.ts: APP_VERSION constant not found');
  else found.push(['frontend/src/constants.ts APP_VERSION', m[1]]);
}

// openapi/openapi.yaml -> info.version (the `version:` key inside `info:`)
{
  const lines = read('openapi', 'openapi.yaml').split(/\r?\n/);
  let inInfo = false;
  let version;
  for (const line of lines) {
    if (/^info:\s*$/.test(line)) {
      inInfo = true;
      continue;
    }
    if (inInfo) {
      if (/^\S/.test(line)) break; // next top-level key ends the info block
      const m = line.match(/^ {2}version:\s*["']?([A-Za-z0-9.\-+]+)["']?\s*$/);
      if (m) {
        version = m[1];
        break;
      }
    }
  }
  if (!version) errors.push('openapi/openapi.yaml: info.version not found');
  else found.push(['openapi/openapi.yaml info.version', version]);
}

// frontend/package.json -> version
{
  try {
    found.push(['frontend/package.json version', JSON.parse(read('frontend', 'package.json')).version]);
  } catch (err) {
    errors.push(`frontend/package.json: unreadable (${err instanceof Error ? err.message : err})`);
  }
}

// src-tauri/Cargo.toml -> [package] version
{
  const lines = read('src-tauri', 'Cargo.toml').split(/\r?\n/);
  let inPackage = false;
  let version;
  for (const line of lines) {
    if (/^\[package\]/.test(line)) {
      inPackage = true;
      continue;
    }
    if (inPackage) {
      if (/^\[/.test(line)) break;
      const m = line.match(/^version\s*=\s*"([^"]+)"/);
      if (m) {
        version = m[1];
        break;
      }
    }
  }
  if (!version) errors.push('src-tauri/Cargo.toml: [package] version not found');
  else found.push(['src-tauri/Cargo.toml [package] version', version]);
}

// Compare everything against the source of truth.
for (const [name, version] of found) {
  if (version !== appVersion) {
    errors.push(`${name} = ${version}, expected ${appVersion}`);
  }
}

const width = Math.max(...found.map(([name]) => name.length));
for (const [name, version] of found) {
  console.log(`  ${name.padEnd(width)}  ${version}`);
}

if (errors.length > 0) {
  console.error(`version check FAILED (${errors.length} mismatch(es)):`);
  for (const err of errors) console.error(`  - ${err}`);
  process.exit(1);
}
console.log(`version check OK: all sources at ${appVersion}`);

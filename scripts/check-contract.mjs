#!/usr/bin/env node
/**
 * Contract lint — CI stage 1 (docs/10 §2 "Contract: OpenAPI + JSON Schema lint").
 *
 * Dependency-free (Node >= 20, no npm packages needed):
 *   1. openapi/openapi.yaml structural assertions:
 *        - declares `openapi: 3.1.0`
 *        - `info.title` is "FloatTranslate Local API"
 *        - every path key in the `paths:` block starts with `/`
 *        - every server URL binds the loopback interface (security invariant,
 *          README 强制架构边界 #6)
 *        - every local `$ref: "#/components/..."` resolves to a definition
 *          that actually exists in the file
 *   2. schemas/*.schema.json parse as JSON and declare a JSON Schema dialect.
 *
 * Exit 0 = contract OK; exit 1 = one or more violations printed.
 */

import { readdirSync, readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..');

/** @type {string[]} */
const errors = [];
const fail = (msg) => errors.push(msg);

// ---------------------------------------------------------------------------
// 1. OpenAPI structural checks
// ---------------------------------------------------------------------------

const openapiRel = join('openapi', 'openapi.yaml');
const openapiText = readFileSync(join(repoRoot, openapiRel), 'utf8');
const openapiLines = openapiText.split(/\r?\n/);

// 1a. version header (first non-empty, non-comment line).
const header = openapiLines.find((l) => l.trim() !== '' && !l.trim().startsWith('#'));
if (header === undefined || !/^openapi:\s*3\.1\.0\s*$/.test(header)) {
  fail(`${openapiRel}: first entry must declare "openapi: 3.1.0" (got: ${header?.trim() ?? '<empty>'})`);
}

// 1b. title.
if (!/^ {2}title: FloatTranslate Local API\s*$/m.test(openapiText)) {
  fail(`${openapiRel}: info.title must be exactly "FloatTranslate Local API"`);
}

// 1c. path keys all start with "/".
let inPaths = false;
let pathCount = 0;
for (const line of openapiLines) {
  if (/^paths:\s*$/.test(line)) {
    inPaths = true;
    continue;
  }
  if (!inPaths) continue;
  if (/^\S/.test(line)) {
    // Next top-level key ends the paths block.
    inPaths = false;
    continue;
  }
  const m = line.match(/^ {2}([^#\s][^:]*):\s*(?:#.*)?$/);
  if (m) {
    const key = m[1].trim();
    if (!key.startsWith('/')) {
      fail(`${openapiRel}: path key "${key}" must start with "/"`);
    }
    pathCount += 1;
  }
}
if (inPaths) fail(`${openapiRel}: unexpected EOF inside paths block`); // defensive
if (pathCount === 0) fail(`${openapiRel}: no paths found under "paths:"`);

// 1d. servers must be loopback-only.
const serverUrls = [...openapiText.matchAll(/^\s*-\s*url:\s*["']?([^"'\s]+)["']?\s*$/gm)].map(
  (m) => m[1],
);
if (serverUrls.length === 0) {
  fail(`${openapiRel}: no servers declared`);
}
for (const url of serverUrls) {
  try {
    const host = new URL(url.replace(/\{[^}]+\}/g, '0')).hostname;
    if (host !== '127.0.0.1' && host !== 'localhost') {
      fail(`${openapiRel}: server "${url}" must bind the loopback interface, got host "${host}"`);
    }
  } catch {
    fail(`${openapiRel}: server "${url}" is not a valid URL`);
  }
}

// 1e. every local $ref resolves inside this file.
const refs = [...openapiText.matchAll(/\$ref:\s*["']([^"']+)["']/g)].map((m) => m[1]);
const escapeRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
const pointerUnescape = (s) => s.replace(/~1/g, '/').replace(/~0/g, '~');
let resolvedRefs = 0;
for (const ref of refs) {
  if (!ref.startsWith('#/')) {
    fail(`${openapiRel}: external $ref "${ref}" is not allowed (contract must stay self-contained)`);
    continue;
  }
  const name = pointerUnescape(ref.slice(2).split('/').pop() ?? '');
  if (name === '') {
    fail(`${openapiRel}: malformed $ref "${ref}"`);
    continue;
  }
  // Direct children of a components section sit at 4-space indent; anything
  // else (e.g. an inline schema name) may live at any indent.
  const pattern = ref.startsWith('#/components/')
    ? new RegExp(`^ {4}${escapeRe(name)}:\\s*($|#)`, 'm')
    : new RegExp(`^\\s*${escapeRe(name)}:\\s*($|#)`, 'm');
  if (!pattern.test(openapiText)) {
    fail(`${openapiRel}: $ref "${ref}" does not resolve (no definition for "${name}")`);
    continue;
  }
  resolvedRefs += 1;
}

// ---------------------------------------------------------------------------
// 2. JSON Schemas
// ---------------------------------------------------------------------------

const schemasDir = join(repoRoot, 'schemas');
let schemaCount = 0;
for (const entry of readdirSync(schemasDir).sort()) {
  if (!entry.endsWith('.schema.json')) continue;
  schemaCount += 1;
  const rel = join('schemas', entry);
  let parsed;
  try {
    parsed = JSON.parse(readFileSync(join(schemasDir, entry), 'utf8'));
  } catch (err) {
    fail(`${rel}: invalid JSON (${err instanceof Error ? err.message : String(err)})`);
    continue;
  }
  if (typeof parsed?.$schema !== 'string' || !parsed.$schema.includes('json-schema.org')) {
    fail(`${rel}: missing or invalid "$schema" dialect declaration`);
  }
  if (typeof parsed?.type !== 'string' && !('$ref' in (parsed ?? {}))) {
    fail(`${rel}: schema must declare a "type" (or a top-level "$ref")`);
  }
}
if (schemaCount === 0) fail('schemas/: no *.schema.json files found');

// ---------------------------------------------------------------------------
// Report
// ---------------------------------------------------------------------------

if (errors.length > 0) {
  console.error(`contract check FAILED (${errors.length} violation(s)):`);
  for (const err of errors) console.error(`  - ${err}`);
  process.exit(1);
}
console.log(
  `contract OK: ${pathCount} paths, ${resolvedRefs}/${refs.length} $refs resolved, ` +
    `${serverUrls.length} loopback server(s), ${schemaCount} JSON schema(s) parsed`,
);

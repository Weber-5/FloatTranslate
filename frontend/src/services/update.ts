/**
 * Update check (docs/10 §5): read the latest stable GitHub Release, compare
 * semantic versions, and only PROMPT — never silently download or install.
 * No dependency: a tiny semver parser covers the `vMAJOR.MINOR.PATCH` tags
 * this project publishes.
 *
 * Frozen endpoint: https://api.github.com/repos/Weber-5/FloatTranslate/releases/latest
 * (Accept: application/vnd.github+json, no auth). Mock mode can inject a
 * simulated latest release so the banner is demoable without network access.
 */

export const RELEASE_API_URL =
  'https://api.github.com/repos/Weber-5/FloatTranslate/releases/latest'

/** GitHub Releases Accept header per docs (no auth, public repo). */
const RELEASE_ACCEPT = 'application/vnd.github+json'

export interface LatestRelease {
  tag_name: string
  html_url?: string
  name?: string
}

export type UpdateCheckStatus = 'update-available' | 'up-to-date' | 'no-release'

export interface UpdateCheckResult {
  status: UpdateCheckStatus
  /** Null when the repo has no published release yet (drafts are invisible
   *  to /releases/latest) — reported as `no-release`, never as an error and
   *  never as a false "up to date" (improvement bug #7). */
  latest: LatestRelease | null
  /** compareSemver(latest, current): negative → latest older, 0 equal, positive newer. */
  comparison: number
}

export class UpdateCheckError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'UpdateCheckError'
  }
}

interface SemVer {
  major: number
  minor: number
  patch: number
}

/** Parses "v1.2.3" / "1.2.3" (pre-release/build suffixes ignored). Null when unparseable. */
export function parseSemver(tag: string): SemVer | null {
  const match = /^\s*v?(\d+)\.(\d+)\.(\d+)/.exec(tag)
  if (!match) return null
  return {
    major: Number(match[1]),
    minor: Number(match[2]),
    patch: Number(match[3]),
  }
}

/**
 * Compares two version strings. Unparseable input compares as equal (fail
 * safe: never claim an update exists when the tag cannot be understood).
 */
export function compareSemver(a: string, b: string): number {
  const left = parseSemver(a)
  const right = parseSemver(b)
  if (!left || !right) return 0
  for (const key of ['major', 'minor', 'patch'] as const) {
    if (left[key] !== right[key]) return left[key] > right[key] ? 1 : -1
  }
  return 0
}

/**
 * Fetches the latest stable release. Returns null when the repo has no
 * published release (HTTP 404 — draft releases are invisible to this
 * endpoint; improvement bug #7). Other network/HTTP failures reject with
 * UpdateCheckError so the UI can offer a retry.
 */
export async function fetchLatestRelease(
  fetchImpl: typeof fetch = fetch,
): Promise<LatestRelease | null> {
  let response: Response
  try {
    response = await fetchImpl(RELEASE_API_URL, {
      headers: { Accept: RELEASE_ACCEPT },
    })
  } catch {
    throw new UpdateCheckError('network error')
  }
  if (response.status === 404) return null
  if (!response.ok) {
    throw new UpdateCheckError(`HTTP ${response.status}`)
  }
  const body = (await response.json().catch(() => null)) as
    | (LatestRelease & { draft?: unknown; prerelease?: unknown })
    | null
  if (!body || typeof body.tag_name !== 'string' || body.tag_name.length === 0) {
    throw new UpdateCheckError('malformed release payload')
  }
  return {
    tag_name: body.tag_name,
    ...(typeof body.html_url === 'string' ? { html_url: body.html_url } : {}),
    ...(typeof body.name === 'string' ? { name: body.name } : {}),
  }
}

/**
 * Compares the latest release against the running version. `simulatedLatest`
 * replaces the network call (mock mode: { tag_name: 'v1.0.1' } when the demo
 * flag is set, so no real request leaves the machine).
 *
 * No published release (HTTP 404, or a repo that is not public yet) is its own
 * outcome: reporting "up to date" there would claim a comparison that never
 * happened (improvement bug #7).
 */
export async function checkForUpdate(
  currentVersion: string,
  simulatedLatest?: LatestRelease,
  fetchImpl: typeof fetch = fetch,
): Promise<UpdateCheckResult> {
  const latest = simulatedLatest ?? (await fetchLatestRelease(fetchImpl))
  if (!latest) {
    return { status: 'no-release', latest: null, comparison: 0 }
  }
  const comparison = compareSemver(latest.tag_name, currentVersion)
  return {
    status: comparison > 0 ? 'update-available' : 'up-to-date',
    latest,
    comparison,
  }
}

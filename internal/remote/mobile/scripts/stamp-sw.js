import { execSync } from 'node:child_process'
import { readFileSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const here = fileURLToPath(new URL('.', import.meta.url))

function buildHash() {
  const fromVercel = process.env.VERCEL_GIT_COMMIT_SHA
  if (fromVercel) return fromVercel.slice(0, 7)
  try {
    return execSync('git rev-parse --short HEAD', { cwd: here + '..', stdio: ['ignore', 'pipe', 'ignore'] })
      .toString()
      .trim()
  } catch {
    return Date.now().toString(36)
  }
}

const hash = buildHash()
const swPath = new URL('../dist/sw.js', import.meta.url)
const src = readFileSync(swPath, 'utf8')
if (src.includes('__BUILD_HASH__')) {
  writeFileSync(swPath, src.replaceAll('__BUILD_HASH__', hash))
  console.log(`stamp-sw: CACHE=orchestrator-shell-${hash}`)
} else if (src.includes('orchestrator-shell-')) {
  writeFileSync(swPath, src.replace(/orchestrator-shell-[a-zA-Z0-9_-]+/g, `orchestrator-shell-${hash}`))
  console.log(`stamp-sw: updated CACHE=orchestrator-shell-${hash}`)
} else {
  console.error('stamp-sw: placeholder __BUILD_HASH__ missing in dist/sw.js')
  process.exit(1)
}

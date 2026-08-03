import { readdirSync, statSync } from 'node:fs'
import { spawnSync } from 'node:child_process'
import { join } from 'node:path'

const check = process.argv[2]

function fail(message) {
  console.error(message)
  process.exit(1)
}

if (check === 'fmt') {
  const result = spawnSync('gofmt', ['-l', '.'], { encoding: 'utf8', shell: false })
  if (result.error) fail(result.error.message)
  if (result.status !== 0) fail(result.stderr || 'gofmt failed')
  const files = result.stdout.trim()
  if (files) fail(`Go files need formatting:\n${files}`)
  process.exit(0)
}

if (check === 'changelog') {
  for (const file of ['CHANGELOG.md', 'LICENSE']) {
    try {
      if (statSync(file).size === 0) fail(`${file} is empty`)
    } catch {
      fail(`${file} is missing`)
    }
  }
  let entries = []
  try {
    entries = readdirSync('.changelog', { withFileTypes: true })
      .filter(entry => entry.isFile() && entry.name.endsWith('.md') && entry.name !== 'README.md')
      .map(entry => join('.changelog', entry.name))
  } catch {
    fail('.changelog is missing')
  }
  if (entries.length === 0) fail('at least one .changelog/*.md entry is required')
  process.exit(0)
}

fail('usage: node scripts/verify-repository.mjs <fmt|changelog>')

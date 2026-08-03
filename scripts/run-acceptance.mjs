import { existsSync } from 'node:fs'
import { delimiter, join } from 'node:path'
import { spawnSync } from 'node:child_process'

const engine = process.argv[2]
const live = process.argv.includes('--live')

if (!['terraform', 'tofu'].includes(engine)) {
  console.error('usage: node scripts/run-acceptance.mjs <terraform|tofu> [--live]')
  process.exit(2)
}

function resolveExecutable(name) {
  const suffixes =
    process.platform === 'win32'
      ? (process.env.PATHEXT || '.EXE;.CMD;.BAT').split(';').map(value => value.toLowerCase())
      : ['']

  for (const directory of (process.env.PATH || '').split(delimiter)) {
    if (!directory) continue
    for (const suffix of suffixes) {
      const candidate = join(directory, process.platform === 'win32' ? `${name}${suffix}` : name)
      if (existsSync(candidate)) return candidate
    }
  }
  throw new Error(`${name} is not installed or is not available on PATH`)
}

let executable
try {
  executable = resolveExecutable(engine)
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error))
  process.exit(1)
}

const env = {
  ...process.env,
  TF_ACC: '1',
  TF_ACC_PROVIDER_HOST: engine === 'tofu' ? 'registry.opentofu.org' : 'registry.terraform.io',
  TF_ACC_PROVIDER_NAMESPACE: 'vappcloud',
  TF_ACC_TERRAFORM_PATH: executable,
  ...(live ? { VAPPCLOUD_REAL_ACC: '1' } : {})
}
const pattern = live ? '^TestAccRealAPI' : '^TestAcc'
const result = spawnSync('go', ['test', '-v', '-timeout', '30m', './internal/provider', '-run', pattern], {
  env,
  stdio: 'inherit',
  shell: false
})

if (result.error) {
  console.error(result.error.message)
  process.exit(1)
}
process.exit(result.status ?? 1)

// Run with: node --import tsx tests/office-encryption/probe.mjs <sample...>
import { readFileSync } from 'node:fs'
import { basename } from 'node:path'
import { File } from 'node:buffer'
import { createHash } from 'node:crypto'
import { inspectOffice } from '../../src/utils/officeEncryption.ts'

if (process.argv.length < 3) process.exit(2)

for (const path of process.argv.slice(2)) {
  const data = readFileSync(path)
  console.log(JSON.stringify({
    file: basename(path), bytes: data.length,
    sha256: createHash('sha256').update(data).digest('hex'),
    classification: await inspectOffice(new File([data], basename(path))),
  }))
}

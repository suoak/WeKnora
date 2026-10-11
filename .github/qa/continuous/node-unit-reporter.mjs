// Trusted node:test events. No error objects, cause, diagnostic text or body.
import { createHash } from 'node:crypto';
const hash = value => createHash('sha256').update(JSON.stringify(value)).digest('hex');
export default async function* reporter(source) {
  for await (const { type, data } of source) {
    if (type === 'test:pass' || type === 'test:fail') {
      const status = data.skip ? 'SKIP' : data.todo ? 'TODO' : type === 'test:pass' ? 'PASS' : 'FAIL';
      yield JSON.stringify({kind: 'TERMINAL', id: hash([data.file, data.line, data.column, data.nesting, data.testNumber, data.name]),
        nameSHA: hash(data.name), testType: data.details.type, status}) + '\n';
    } else if (type === 'test:summary' && !data.file) {
      yield JSON.stringify({kind: 'SUMMARY', success: data.success, counts: data.counts}) + '\n';
    }
  }
}

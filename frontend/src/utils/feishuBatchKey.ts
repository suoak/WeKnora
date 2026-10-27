// randomUUID requires HTTPS. getRandomValues also works on the explicitly
// allowed isolated HTTP test origin; the server still validates idempotency.
export function feishuBatchKey(random: Pick<Crypto, 'getRandomValues'> = globalThis.crypto): string {
  const bytes = random.getRandomValues(new Uint8Array(16))
  return Array.from(bytes, value => value.toString(16).padStart(2, '0')).join('')
}

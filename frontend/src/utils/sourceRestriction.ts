export function isSourceBlockedProcessing(status: string | undefined, restricted: boolean): boolean {
  return restricted && ['pending', 'processing', 'finalizing'].includes(status || '');
}

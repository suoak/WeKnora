// The existing API only emits this field when SourceID is enabled. Provider
// display names are free text and must not be used to relabel other providers.
export function isSourceIDProvider(config: { sourceid_basic_encoding?: string }): boolean {
  return config.sourceid_basic_encoding === 'oauth_form' || config.sourceid_basic_encoding === 'raw'
}

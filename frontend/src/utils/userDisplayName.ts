type DisplayUser = { username?: string; email?: string } | null | undefined

const generatedOAName = /^oa-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

// Presentation only. Account IDs, employee bindings and authorization continue
// to use the server's original values; a display name is not an identity key.
export function getUserDisplayName(user: DisplayUser): string {
  const username = user?.username?.trim() || ''
  if (!generatedOAName.test(username)) return username
  const email = user?.email?.trim() || ''
  const match = /^([^@\s]+)@[^@\s]+$/.exec(email)
  return match?.[1] || username
}

export function getWorkspaceDisplayName(name: string, user: DisplayUser, owner: boolean): string {
  const username = user?.username?.trim() || ''
  if (!owner || !generatedOAName.test(username)) return name
  if (name !== username && name !== `${username}'s Workspace`) return name
  return name.replace(username, getUserDisplayName(user))
}

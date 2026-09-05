export interface InviteRegistrationState {
  registrationEnabled: boolean
  isRegisterMode: boolean
}

/**
 * A validated invitation token authorizes account creation even when public
 * self-service registration is disabled. Keep the public-registration flag
 * separate so an invitation never re-enables the ordinary registration path.
 */
export function resolveInviteRegistrationState(registrationMode: string): InviteRegistrationState {
  return {
    registrationEnabled: registrationMode !== 'invite_only',
    isRegisterMode: true,
  }
}

package types

// UserDisplayName keeps creator badges consistent. Callers exposing a public
// display name can omit Email to avoid the authenticated UI's email fallback.
func UserDisplayName(u *User) string {
	if u == nil {
		return ""
	}
	if u.Username != "" {
		return u.Username
	}
	return u.Email
}

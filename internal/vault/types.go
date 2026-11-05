package vault

import "time"

// Login is a vaulted website login (add-browser-login-handover design
// D5): the stored metadata plus the decrypted password. Password is the
// plaintext secret — it exists only between the sealed blob and its
// single consumer, and json:"-" makes even an accidental marshal of the
// domain type structurally safe (nothing may serialize the secret).
type Login struct {
	ID       string
	Name     string
	Origin   string
	Username string
	// AddedBy records who handed the secret over (the agent name, or the
	// user through settings).
	AddedBy string
	// Password is the plaintext secret. Never logged, never serialized.
	Password  string `json:"-"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// LoginView is the login's redacted projection for listings and surfaces
// (the CredentialSet discipline): it carries no secret material, so it
// may cross to any consumer.
type LoginView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Origin   string `json:"origin"`
	AddedBy  string `json:"added_by"`
}

// View projects the login onto its redacted view: the password is dropped
// at the type boundary.
func (l Login) View() LoginView {
	return LoginView{
		ID:       l.ID,
		Name:     l.Name,
		Username: l.Username,
		Origin:   l.Origin,
		AddedBy:  l.AddedBy,
	}
}

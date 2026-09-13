package portalwebauthn

import (
	"github.com/go-webauthn/webauthn/webauthn"

	"windshift/internal/webauthn/persistence"
)

// CredentialStore handles portal-customer WebAuthn credentials while the
// shared persistence package owns SQL, serialization, and authenticator
// updates.
type CredentialStore struct {
	store *persistence.CredentialStore
}

// NewCredentialStore creates a store bound to the portal-customer schema.
func NewCredentialStore(db Database) *CredentialStore {
	return &CredentialStore{store: persistence.NewPortalCredentialStore(db)}
}

// SaveCredential stores a new WebAuthn credential for a portal customer.
func (cs *CredentialStore) SaveCredential(portalCustomerID int, credentialName string, cred *webauthn.Credential) error {
	return cs.store.SaveCredential(portalCustomerID, credentialName, cred)
}

// GetCustomerCredentials retrieves credentials in go-webauthn format.
func (cs *CredentialStore) GetCustomerCredentials(portalCustomerID int) ([]webauthn.Credential, error) {
	return cs.store.GetCredentials(portalCustomerID)
}

// UpdateCredentialCounter persists the post-login sign count and clone flag.
func (cs *CredentialStore) UpdateCredentialCounter(credentialID []byte, signCount uint32, cloneWarning bool) error {
	return cs.store.UpdateCredentialCounter(credentialID, signCount, cloneWarning)
}

// DeleteCredential removes a credential by its encoded ID.
func (cs *CredentialStore) DeleteCredential(credentialID string) error {
	return cs.store.DeleteCredential(credentialID)
}

// GetCustomerCredentialsList returns display-friendly credential rows.
func (cs *CredentialStore) GetCustomerCredentialsList(portalCustomerID int) ([]Credential, error) {
	records, err := cs.store.GetCredentialRecords(portalCustomerID)
	if err != nil {
		return nil, err
	}
	credentials := make([]Credential, 0, len(records))
	for _, record := range records {
		credentials = append(credentials, portalCredentialFromRecord(record))
	}
	return credentials, nil
}

// CheckCredentialExists reports whether a credential ID is already stored.
func (cs *CredentialStore) CheckCredentialExists(credentialID []byte) (bool, error) {
	return cs.store.CheckCredentialExists(credentialID)
}

package bit

import "time"

// Ref tracks a Bit's reference in an external system.
// Bidirectional hash tracking enables sync loop prevention:
// - TheirHash: Hash of their (external system's) state when we last fetched
// - OurHash: Hash we last pushed to them
//
// If TheirHash != OurHash after a fetch, the external system changed.
// If TheirHash == OurHash, we're in sync.
type Ref struct {
	MasterURI  string    `json:"master_uri"`  // FK to Bit's canonical URI
	System     string    `json:"system"`      // github, thymer, obsidian, etc.
	ExternalID string    `json:"external_id"` // ID in that system (e.g., Thymer UUID)
	TheirHash  string    `json:"their_hash"`  // Hash of their state
	OurHash    string    `json:"our_hash"`    // Hash we last sent
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// NewRef creates a new Ref.
func NewRef(masterURI, system, externalID string) *Ref {
	now := time.Now()
	return &Ref{
		MasterURI:  masterURI,
		System:     system,
		ExternalID: externalID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// NeedsPull returns true if the external system has changes we haven't pulled.
// This happens when their hash differs from ours (they changed since our last push).
func (r *Ref) NeedsPull() bool {
	return r.TheirHash != "" && r.TheirHash != r.OurHash
}

// NeedsPush returns true if we have local changes not yet pushed.
// This is determined by comparing our local bit's hash against OurHash.
func (r *Ref) NeedsPush(localHash string) bool {
	return r.OurHash != localHash
}

// IsInSync returns true if both sides have the same content.
func (r *Ref) IsInSync() bool {
	return r.TheirHash == r.OurHash && r.TheirHash != ""
}

// MarkPushed updates the ref after successfully pushing to the external system.
func (r *Ref) MarkPushed(hash string) {
	r.OurHash = hash
	r.TheirHash = hash // Assume they now have what we pushed
	r.UpdatedAt = time.Now()
}

// MarkPulled updates the ref after successfully pulling from the external system.
func (r *Ref) MarkPulled(hash string) {
	r.TheirHash = hash
	r.UpdatedAt = time.Now()
}

// Note: System constants are defined in bit.go

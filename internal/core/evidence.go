package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const DefaultPolicyVersion = "offboarding-policy/v1"

// EvidenceItem is a product-level proof artifact, not a debug log line.
type EvidenceItem struct {
	ID                 string            `json:"id"`
	TenantID           string            `json:"tenant_id,omitempty"`
	SourceProvider     string            `json:"source_provider,omitempty"`
	SourceEndpoint     string            `json:"source_endpoint,omitempty"`
	ObjectKind         ObjectKind        `json:"object_kind,omitempty"`
	ObjectID           string            `json:"object_id,omitempty"`
	CollectedAt        time.Time         `json:"collected_at"`
	ProviderTimestamp  *time.Time        `json:"provider_timestamp,omitempty"`
	Actor              string            `json:"actor,omitempty"`
	ScopesUsed         []string          `json:"scopes_used,omitempty"`
	PolicyVersion      string            `json:"policy_version,omitempty"`
	BeforeSnapshotHash string            `json:"before_snapshot_hash,omitempty"`
	AfterSnapshotHash  string            `json:"after_snapshot_hash,omitempty"`
	RedactionSummary   string            `json:"redaction_summary,omitempty"`
	KnownLimits        []string          `json:"known_limits,omitempty"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

// NewEvidenceItem fills the fields that should be present on every collected
// proof item.
func NewEvidenceItem(provider string, objectKind ObjectKind, objectID string, collectedAt time.Time, payload any) EvidenceItem {
	if collectedAt.IsZero() {
		collectedAt = time.Now().UTC()
	}
	hash := HashEvidencePayload(payload)
	return EvidenceItem{
		ID:                 EvidenceID(provider, objectKind, objectID, collectedAt),
		SourceProvider:     provider,
		ObjectKind:         objectKind,
		ObjectID:           objectID,
		CollectedAt:        collectedAt.UTC(),
		Actor:              "system",
		PolicyVersion:      DefaultPolicyVersion,
		BeforeSnapshotHash: hash,
		RedactionSummary:   "structured provider response redacted before display; hash retained for comparison",
	}
}

// EvidenceID is deterministic enough for human-readable local artifacts while
// still varying by collection time.
func EvidenceID(provider string, objectKind ObjectKind, objectID string, collectedAt time.Time) string {
	base := strings.Join([]string{provider, string(objectKind), objectID, collectedAt.UTC().Format(time.RFC3339Nano)}, ":")
	sum := sha256.Sum256([]byte(base))
	return "ev_" + hex.EncodeToString(sum[:])[:16]
}

// HashEvidencePayload returns a stable hash of a structured payload. It never
// returns raw provider data.
func HashEvidencePayload(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		data = []byte(fmt.Sprintf("%T", v))
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

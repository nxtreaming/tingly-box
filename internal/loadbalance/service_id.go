package loadbalance

import "fmt"

// ServiceID uniquely identifies a provider+model combination in load balancing.
type ServiceID struct {
	ProviderUUID string `json:"provider_uuid"`
	Model        string `json:"model"`
}

// NewServiceID creates a ServiceID from provider UUID/name and model.
func NewServiceID(providerUUID, model string) ServiceID {
	return ServiceID{ProviderUUID: providerUUID, Model: model}
}

// String returns a stable string for use as map key and logging.
func (id ServiceID) String() string {
	return FormatServiceID(id.ProviderUUID, id.Model)
}

// FormatServiceID formats a (providerUUID, model) pair into the canonical
// "provider/model" string used as the exclusion map key and as the
// service-scoped half of the breaker key. Sites that don't have a full
// *Service in hand (the failover orchestrator, the recorder, usage tracking)
// call this directly so all paths agree on the same key shape.
func FormatServiceID(providerUUID, model string) string {
	return fmt.Sprintf("%s/%s", providerUUID, model)
}

// FormatBreakerKey formats a (ruleUUID, serviceID) pair into the canonical
// "ruleUUID/serviceID" string used as the breaker-store key. The breaker is
// rule-scoped: each rule owns independent breaker state per service so a busy
// rule's failing traffic cannot trip another rule's primary. The serviceID
// half is FormatServiceID's "provider/model"; rule UUIDs are slash-free, so
// the composite is unambiguous. Mirrors the affinity store's rule-scoped key
// (internal/server/affinity/affinity.go makeKey).
func FormatBreakerKey(ruleUUID, serviceID string) string {
	return fmt.Sprintf("%s/%s", ruleUUID, serviceID)
}

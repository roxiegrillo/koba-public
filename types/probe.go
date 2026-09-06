package types

// ProbeStatus is the stable response envelope returned by the framework-owned
// liveness and readiness endpoints.
type ProbeStatus struct {
	Status string `json:"status" binding:"required"`
}

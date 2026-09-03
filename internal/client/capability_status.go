package client

import (
	"context"
	"net/url"
)

// InstanceCapabilityStatus is what the agent last reported it CAN collect, as
// opposed to what the operator switched on (the *_enabled flags on the instance
// itself). It is what tells "Redis monitoring is enabled" from "Redis
// monitoring is enabled and working".
//
// THE HONESTY RULE: an empty Capabilities map means "NOT REPORTED", never
// "nothing is supported". A reader that collapses those two says "Docker is
// unsupported here" about a host nobody has heard from.
//
// And the rule does not key off UpdatedAt. The server stamps it on every
// check-in whether or not the agent sent a capability block, so an older agent
// checking in every 60s presents as an empty map with a timestamp seconds old.
// UpdatedAt is null only for a host that has never posted anything.
type InstanceCapabilityStatus struct {
	// Capabilities is the per-capability verdict from the agent.
	Capabilities map[string]bool `json:"capabilities"`
	// Pending is the capabilities the operator enabled that the agent cannot
	// yet collect. It is UNGATED here: this reports what the agent last said,
	// so a feature disabled since the last tick lingers until the next one.
	Pending []string `json:"pending"`
	// Reasons is the agent's explanation for a blocked capability, truncated to
	// 500 characters and stripped of control characters server-side.
	Reasons map[string]string `json:"reasons"`
	// UpdatedAt is null only for a host that has never posted ANY payload.
	UpdatedAt *string `json:"updated_at"`
}

// GetInstanceCapabilityStatus reads GET /api/v1/instances/{id}/capability_status.
func (c *Client) GetInstanceCapabilityStatus(ctx context.Context, instanceID string) (*InstanceCapabilityStatus, error) {
	var result struct {
		CapabilityStatus InstanceCapabilityStatus `json:"capability_status"`
	}
	path := "/api/v1/instances/" + url.PathEscape(instanceID) + "/capability_status"
	if err := c.getJSON(ctx, path, &result); err != nil {
		return nil, err
	}
	return &result.CapabilityStatus, nil
}

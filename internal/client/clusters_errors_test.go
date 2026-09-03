package client

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// Every cluster and status endpoint reads through getJSON, so a non-200 has to
// come back as an APIError from all five rather than as an empty result. An
// index that swallowed the error would hand Terraform "this organization has no
// clusters" during an outage.
func TestClusterEndpoints_ErrorsPropagate(t *testing.T) {
	for _, tt := range []struct {
		name string
		call func(c *Client) error
	}{
		{"ListCephClusters", func(c *Client) error {
			_, err := c.ListCephClusters(context.Background(), CephClusterListOptions{})
			return err
		}},
		{"GetCephCluster", func(c *Client) error {
			_, err := c.GetCephCluster(context.Background(), "8e4a-prod")
			return err
		}},
		{"ListProxmoxClusters", func(c *Client) error {
			_, err := c.ListProxmoxClusters(context.Background(), ProxmoxClusterListOptions{})
			return err
		}},
		{"GetProxmoxCluster", func(c *Client) error {
			_, err := c.GetProxmoxCluster(context.Background(), "cluster-uuid")
			return err
		}},
		{"GetInstanceCapabilityStatus", func(c *Client) error {
			_, err := c.GetInstanceCapabilityStatus(context.Background(), "host-uuid")
			return err
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error": "Internal Server Error"}`))
			})

			err := tt.call(c)
			if err == nil {
				t.Fatal("expected an error for a 500")
			}
			apiErr, ok := err.(*APIError)
			if !ok || apiErr.StatusCode != http.StatusInternalServerError {
				t.Errorf("expected a 500 APIError, got %#v", err)
			}
		})
	}
}

// A 200 carrying a body getJSON cannot decode is an error, not a zero-valued
// cluster: the callers publish every field of what they get back, so a silently
// empty struct would populate Terraform state with blanks.
func TestGetJSON_MalformedBodyIsAnError(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ceph_cluster": `))
	})

	_, err := c.GetCephCluster(context.Background(), "8e4a-prod")
	if err == nil {
		t.Fatal("expected an error for an undecodable body")
	}
	if !strings.Contains(err.Error(), "decoding response") {
		t.Errorf("expected a decode error, got %v", err)
	}
}

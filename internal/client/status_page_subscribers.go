package client

import (
	"context"
	"net/url"
	"strconv"
)

// subscriberPerPage is the maximum per_page the subscriber index accepts.
const subscriberPerPage = 100

// StatusPageSubscriber is one email subscriber of a status page.
//
// Confirmation and unsubscribe tokens are bearer capabilities on the public
// page -- anyone holding the unsubscribe token can drop that address -- so the
// API never returns them and this struct has no field for them.
type StatusPageSubscriber struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	// Status is "confirmed" or "pending". A pending address has been sent a
	// confirmation email and has not clicked it, and receives no notifications.
	Status      string  `json:"status"`
	ConfirmedAt *string `json:"confirmed_at"`
	CreatedAt   *string `json:"created_at"`
	UpdatedAt   *string `json:"updated_at"`
}

// StatusPageSubscriberListOptions narrows the subscriber index.
type StatusPageSubscriberListOptions struct {
	Query        string
	UpdatedSince string
	Status       string
	Order        string
	Direction    string
}

func (o StatusPageSubscriberListOptions) query() url.Values {
	q := url.Values{}
	for key, value := range map[string]string{
		"q":             o.Query,
		"updated_since": o.UpdatedSince,
		"status":        o.Status,
		"order":         o.Order,
		"direction":     o.Direction,
	} {
		if value != "" {
			q.Set(key, value)
		}
	}
	return q
}

// ListStatusPageSubscribers returns a status page's email subscribers.
//
// REQUIRES THE `status_pages: update` PERMISSION even though it only reads:
// subscriber emails are PII, so a read-only token gets a 403. That is why the
// data source surfaces the 403 with its own diagnostic rather than an empty
// list.
func (c *Client) ListStatusPageSubscribers(ctx context.Context, statusPageID int64, opts StatusPageSubscriberListOptions) ([]StatusPageSubscriber, error) {
	base := "/api/v1/status_pages/" + strconv.FormatInt(statusPageID, 10) + "/subscribers"
	return listAllPages[StatusPageSubscriber](ctx, c, base, "subscribers", opts.query(), subscriberPerPage)
}

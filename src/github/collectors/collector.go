// Package collectors implements GitHub API data collectors that poll
// specific endpoint families and return typed results.
package collectors

import "context"

// maxPages caps pagination loops to prevent unbounded API consumption.
const maxPages = 100

// APIClient is the interface used by collectors to make GitHub API calls.
// It breaks the import cycle between the github and collectors packages.
type APIClient interface {
	Get(ctx context.Context, url string, result interface{}) (modified bool, err error)
}

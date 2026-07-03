package modelmeta

import "net/http"

// SetHTTPClient allows overriding the package-level httpClient during tests.
func SetHTTPClient(client *http.Client) {
	httpClient = client
}

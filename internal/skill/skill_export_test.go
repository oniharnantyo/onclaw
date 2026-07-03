package skill

import "net/http"

func ClassifySourceType(source string) string {
	return classifySourceType(source)
}

func SetGithubHTTPClient(c *http.Client) {
	githubHttpClient = c
}

func SetHTTPClient(c *http.Client) {
	httpClient = c
}

func DetectPlugin(dir string) bool {
	return detectPlugin(dir)
}

package tools

// browserArgs is the deserialized tool-call argument shape of the browser
// tool. Every field except Action is optional; which fields an action
// requires is validated per action.
type browserArgs struct {
	Action       string `json:"action"`
	Profile      string `json:"profile,omitempty"`
	URL          string `json:"url,omitempty"`
	Ref          string `json:"ref,omitempty"`
	Text         string `json:"text,omitempty"`
	Key          string `json:"key,omitempty"`
	Direction    string `json:"direction,omitempty"`
	CookieOp     string `json:"cookie_op,omitempty"`
	CookieName   string `json:"cookie_name,omitempty"`
	CookieValue  string `json:"cookie_value,omitempty"`
	CookieDomain string `json:"cookie_domain,omitempty"`
	CookiePath   string `json:"cookie_path,omitempty"`
}

// browserSnapshotElement is one interactive element of a snapshot; Ref
// addresses the element in click/type until the next navigation.
type browserSnapshotElement struct {
	Ref   string `json:"ref"`
	Role  string `json:"role"`
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

// browserNavigateResult is the navigate result payload.
type browserNavigateResult struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// browserSnapshotResult is the snapshot result payload.
type browserSnapshotResult struct {
	URL      string                   `json:"url"`
	Title    string                   `json:"title"`
	Elements []browserSnapshotElement `json:"elements"`
}

// browserActionResult is the payload of every state-changing action
// (click/type/press_key/scroll/back): confirmation plus a fresh snapshot so
// the model sees the post-action page without a second call.
type browserActionResult struct {
	OK       bool                     `json:"ok"`
	URL      string                   `json:"url"`
	Title    string                   `json:"title"`
	Elements []browserSnapshotElement `json:"elements"`
}

// browserScreenshotResult is the screenshot result payload.
type browserScreenshotResult struct {
	Path   string `json:"path"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// browserCookie is one cookie of the profile's jar as returned by cookies list.
type browserCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
}

// browserCookiesResult is the cookies list result payload.
type browserCookiesResult struct {
	Cookies []browserCookie `json:"cookies"`
}

// browserOKResult is the payload of mutations that only report success
// (cookies set/delete/clear).
type browserOKResult struct {
	OK bool `json:"ok"`
}

// browserStoredCookie is one cookie of the profile's on-disk jar file
// (buddies-cookies.json). It mirrors the CDP fields needed to re-inject the
// cookie after a browser restart; SameSite is kept as its string form.
type browserStoredCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	Secure   bool    `json:"secure"`
	HTTPOnly bool    `json:"httpOnly"`
	SameSite string  `json:"sameSite,omitempty"`
}

// browserCookieJarFile is the shape of the profile's jar file.
type browserCookieJarFile struct {
	Cookies []browserStoredCookie `json:"cookies"`
}

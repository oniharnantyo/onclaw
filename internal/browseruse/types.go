package browseruse

// SnapshotElement is one interactive element of a snapshot; Ref addresses
// the element in click/type until the next navigation.
type SnapshotElement struct {
	Ref   string `json:"ref"`
	Role  string `json:"role"`
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

// NavigateResult is the navigate result payload.
type NavigateResult struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// SnapshotResult is the snapshot result payload.
type SnapshotResult struct {
	URL      string            `json:"url"`
	Title    string            `json:"title"`
	Elements []SnapshotElement `json:"elements"`
}

// SlimActionResult is the payload of the interactive actions
// (click/type/press_key/scroll/back): confirmation plus the final URL and
// title, and only the elements the action changed — a fresh full list rides
// snapshot/read instead.
type SlimActionResult struct {
	OK      bool              `json:"ok"`
	URL     string            `json:"url"`
	Title   string            `json:"title"`
	Changed []SnapshotElement `json:"changed"`
}

// ScreenshotResult is the screenshot result payload.
type ScreenshotResult struct {
	Path   string `json:"path"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// ReadResult is the read result payload: the page's text bounded to the
// requested length, with the truncation indication when the text was cut.
type ReadResult struct {
	OK        bool   `json:"ok"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	Length    int    `json:"length"`
}

// WaitResult is the wait result payload: the settle finished and the page's
// current URL.
type WaitResult struct {
	OK  bool   `json:"ok"`
	URL string `json:"url"`
}

// Cookie is one cookie of the profile's jar as returned by cookies list.
type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
}

// CookiesResult is the cookies list result payload.
type CookiesResult struct {
	Cookies []Cookie `json:"cookies"`
}

// OKResult is the payload of mutations that only report success
// (cookies set/delete/clear).
type OKResult struct {
	OK bool `json:"ok"`
}

// StoredCookie is one cookie of the profile's on-disk jar file
// (buddies-cookies.json). It mirrors the CDP fields needed to re-inject the
// cookie after a browser restart; SameSite is kept as its string form.
type StoredCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	Secure   bool    `json:"secure"`
	HTTPOnly bool    `json:"httpOnly"`
	SameSite string  `json:"sameSite,omitempty"`
}

// CookieJarFile is the shape of the profile's jar file.
type CookieJarFile struct {
	Cookies []StoredCookie `json:"cookies"`
}

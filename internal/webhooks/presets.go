package webhooks

// Preset is a named sender configuration that seeds an endpoint's verifier
// list. Every preset leaves the secret empty for the user to fill in; Hint
// says where the sender exposes it, and the wizard renders each preset as a
// one-secret-field card over the seeded rows.
type Preset struct {
	// Name is the stable preset label stored on the seeded verifier rows
	// and attributed in the delivery log.
	Name string
	// Title is the human preset name shown in the wizard gallery.
	Title string
	// Hint tells the user where to find the secret in the sender's UI.
	Hint string
	// Verifiers is the ordered verifier list the preset seeds.
	Verifiers []VerifierConfig
}

// Presets lists the sender presets in gallery order.
func Presets() []Preset {
	return []Preset{
		{
			Name:  "gitlab",
			Title: "GitLab",
			Hint:  "GitLab: project or group > Settings > Webhooks > edit the webhook's Secret token",
			Verifiers: []VerifierConfig{
				{Kind: KindStdWeb, Preset: "gitlab"},
			},
		},
		{
			Name:  "github",
			Title: "GitHub",
			Hint:  "GitHub: repo Settings > Webhooks > Add webhook > Secret",
			Verifiers: []VerifierConfig{
				{
					Kind:       KindRawHMAC,
					Preset:     "github",
					HeaderName: "X-Hub-Signature-256",
					Algo:       AlgoSHA256,
					Encoding:   EncHex,
					Prefix:     "sha256=",
				},
			},
		},
		{
			Name:  "stripe",
			Title: "Stripe",
			Hint:  "Stripe Dashboard: Developers > Webhooks > endpoint's signing secret (whsec_...)",
			Verifiers: []VerifierConfig{
				{Kind: KindTsHMAC, Preset: "stripe", Scheme: SchemeStripe, HeaderName: "Stripe-Signature"},
			},
		},
		{
			Name:  "slack",
			Title: "Slack",
			Hint:  "Slack: api.slack.com/apps > your app > Basic Information > App Credentials > Signing Secret",
			Verifiers: []VerifierConfig{
				{
					Kind:            KindTsHMAC,
					Preset:          "slack",
					Scheme:          SchemeV0,
					HeaderName:      "X-Slack-Signature",
					TimestampHeader: "X-Slack-Request-Timestamp",
				},
			},
		},
		{
			Name:  "zoom",
			Title: "Zoom",
			Hint:  "Zoom Marketplace: your app > Features > Zoom Webhook > Webhook Secret Token",
			Verifiers: []VerifierConfig{
				{
					Kind:            KindTsHMAC,
					Preset:          "zoom",
					Scheme:          SchemeV0,
					HeaderName:      "x-zm-signature",
					TimestampHeader: "x-zm-request-timestamp",
				},
			},
		},
		{
			Name:  "shopify",
			Title: "Shopify",
			Hint:  "Shopify admin: Settings > Notifications > Webhooks > edit the subscription's signature (the app's API secret key)",
			Verifiers: []VerifierConfig{
				{
					Kind:       KindRawHMAC,
					Preset:     "shopify",
					HeaderName: "X-Shopify-Hmac-Sha256",
					Algo:       AlgoSHA256,
					Encoding:   EncBase64,
				},
			},
		},
		{
			Name:  "linear",
			Title: "Linear",
			Hint:  "Linear: Settings > API > Webhooks > the webhook's signing secret",
			Verifiers: []VerifierConfig{
				{
					Kind:       KindRawHMAC,
					Preset:     "linear",
					HeaderName: "Linear-Signature",
					Algo:       AlgoSHA256,
					Encoding:   EncHex,
				},
			},
		},
		{
			Name:  "header",
			Title: "Header",
			Hint:  "Any sender that can set a fixed secret header (e.g. GitLab's legacy X-Gitlab-Token): enter the header name and the exact value",
			Verifiers: []VerifierConfig{
				{Kind: KindHeader, Preset: "header", HeaderName: "X-Webhook-Token"},
			},
		},
	}
}

// PresetByName returns the preset with the given stable label.
func PresetByName(name string) (Preset, bool) {
	for _, preset := range Presets() {
		if preset.Name == name {
			return preset, true
		}
	}
	return Preset{}, false
}

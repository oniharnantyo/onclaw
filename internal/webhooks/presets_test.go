package webhooks

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPresetsCatalog(t *testing.T) {
	presets := Presets()
	require.Len(t, presets, 8)
	names := make([]string, 0, len(presets))
	seen := map[string]bool{}
	for _, preset := range presets {
		names = append(names, preset.Name)
		require.False(t, seen[preset.Name], "duplicate preset name %q", preset.Name)
		seen[preset.Name] = true
		require.NotEmpty(t, preset.Title, "preset %s needs a display title", preset.Name)
		require.NotEmpty(t, preset.Hint, "preset %s needs a where-to-find-it hint", preset.Name)
		require.NotEmpty(t, preset.Verifiers, "preset %s must seed at least one verifier", preset.Name)
		require.NoError(t, ValidateVerifierSet(preset.Verifiers), "preset %s rows must validate", preset.Name)
		for _, cfg := range preset.Verifiers {
			require.Empty(t, cfg.Secret, "preset %s leaves the secret to the user", preset.Name)
			require.Equal(t, preset.Name, cfg.Preset, "preset %s rows carry the preset label", preset.Name)
		}
	}
	require.Equal(t, []string{"gitlab", "github", "stripe", "slack", "zoom", "shopify", "linear", "header"}, names)
}

func TestPresetByName(t *testing.T) {
	preset, ok := PresetByName("github")
	require.True(t, ok)
	require.Equal(t, "GitHub", preset.Title)
	require.NotEmpty(t, preset.Hint)

	_, ok = PresetByName("nonexistent")
	require.False(t, ok)
}

// TestPresetVectorsAuthenticate walks every preset: with its documented
// vector (signed independently in vectors_test.go) and the user's secret
// filled in, the seeded verifier set must authenticate and attribute the
// delivery to the preset label. A wrong secret must refuse; for HMAC
// presets a tampered body must refuse too.
func TestPresetVectorsAuthenticate(t *testing.T) {
	vectors := []struct {
		presetName string
		secret     string
		headers    map[string]string
	}{
		{
			presetName: "gitlab",
			secret:     vecSecretStdWeb,
			headers: map[string]string{
				StdWebIDHeader:        vecID,
				StdWebTimestampHeader: vecTSStr,
				StdWebSignatureHeader: refStdWebSign(vecSecretStdWeb, vecID, vecTS, vecBody),
			},
		},
		{
			presetName: "github",
			secret:     vecSecretGitHub,
			headers: map[string]string{
				"X-Hub-Signature-256": refGitHubSign(vecSecretGitHub, vecBody),
			},
		},
		{
			presetName: "stripe",
			secret:     vecSecretStripe,
			headers: map[string]string{
				"Stripe-Signature": refStripeSign(vecSecretStripe, vecTS, vecBody),
			},
		},
		{
			presetName: "slack",
			secret:     vecSecretSlack,
			headers: map[string]string{
				"X-Slack-Signature":         refSlackSign(vecSecretSlack, vecTS, vecBody),
				"X-Slack-Request-Timestamp": vecTSStr,
			},
		},
		{
			presetName: "zoom",
			secret:     vecSecretZoom,
			headers: map[string]string{
				"x-zm-signature":         refZoomSign(vecSecretZoom, vecTS, vecBody),
				"x-zm-request-timestamp": vecTSStr,
			},
		},
		{
			presetName: "shopify",
			secret:     vecSecretShopify,
			headers: map[string]string{
				"X-Shopify-Hmac-Sha256": refShopifySign(vecSecretShopify, vecBody),
			},
		},
		{
			presetName: "linear",
			secret:     vecSecretLinear,
			headers: map[string]string{
				"Linear-Signature": refLinearSign(vecSecretLinear, vecBody),
			},
		},
		{
			presetName: "header",
			secret:     vecHeaderVal,
			headers: map[string]string{
				"X-Webhook-Token": vecHeaderVal,
			},
		},
	}
	tampered := []byte(`{"event":"push","project":{"name":"buddies"},"ref":"refs/heads/main","evil":1}`)
	for _, vec := range vectors {
		t.Run(vec.presetName, func(t *testing.T) {
			preset, ok := PresetByName(vec.presetName)
			require.True(t, ok)

			seeded := make([]VerifierConfig, len(preset.Verifiers))
			for i, cfg := range preset.Verifiers {
				cfg.Secret = vec.secret
				seeded[i] = cfg
			}
			in := signedInput(vec.headers, vecBody)
			label, verdict := Authorize(seeded, in)
			require.Equal(t, VerdictPass, verdict)
			require.Equal(t, vec.presetName, label)

			// Rotate in a wrong secret: nothing may authenticate.
			wrong := make([]VerifierConfig, len(preset.Verifiers))
			for i, cfg := range preset.Verifiers {
				cfg.Secret = "definitely-not-the-secret"
				wrong[i] = cfg
			}
			_, verdict = Authorize(wrong, in)
			require.Equal(t, VerdictFail, verdict)

			// HMAC presets bind the signature to the exact bytes.
			if seeded[0].Kind != KindHeader {
				_, verdict = Authorize(seeded, signedInput(vec.headers, tampered))
				require.Equal(t, VerdictFail, verdict)
			}
		})
	}
}

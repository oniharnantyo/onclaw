package services

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// recordingOpener is a hand-written URLOpener fake: it records every opened
// URL for assertions.
type recordingOpener struct {
	urls []string
}

func (o *recordingOpener) Open(url string) error {
	o.urls = append(o.urls, url)
	return nil
}

func TestNoBrowser_Open_ReturnsErrNoBrowser(t *testing.T) {
	err := NoBrowser{}.Open("https://example")
	require.ErrorIs(t, err, ErrNoBrowser)
}

func TestRecordingOpener_Open_Records(t *testing.T) {
	op := &recordingOpener{}

	require.NoError(t, op.Open("https://example"))

	require.Equal(t, []string{"https://example"}, op.urls)
}

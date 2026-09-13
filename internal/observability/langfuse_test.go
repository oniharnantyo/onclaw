package observability

import (
	"strings"
	"testing"
)

func TestNewLangfuseTraceHandlerUnconfigured(t *testing.T) {
	// Absent configuration means the capability does not exist (D1): no
	// handler, no error, nothing to wire.
	handle, err := NewLangfuseTraceHandler(LangfuseConfig{})
	if err != nil {
		t.Fatalf("unconfigured construction returned error: %v", err)
	}
	if handle != nil {
		t.Fatalf("unconfigured construction returned handle %v, want nil", handle)
	}
}

func TestNewLangfuseTraceHandlerPartialConfigError(t *testing.T) {
	cases := []struct {
		name string
		cfg  LangfuseConfig
	}{
		{"keys without host", LangfuseConfig{PublicKey: "pk-lf-test", SecretKey: "sk-lf-test"}},
		{"host without public key", LangfuseConfig{Host: "https://langfuse.example.com", SecretKey: "sk-lf-test"}},
		{"host without secret key", LangfuseConfig{Host: "https://langfuse.example.com", PublicKey: "pk-lf-test"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handle, err := NewLangfuseTraceHandler(tc.cfg)
			if err == nil {
				t.Fatalf("partial configuration accepted (handle: %v)", handle)
			}
			if !strings.Contains(err.Error(), "ONCLAW_LANGFUSE_") {
				t.Fatalf("error %q does not name the ONCLAW_LANGFUSE_* variables", err)
			}
			if handle != nil {
				t.Fatalf("partial configuration returned handle %v, want nil", handle)
			}
		})
	}
}

func TestNewLangfuseTraceHandlerInvalidHost(t *testing.T) {
	for _, host := range []string{"langfuse.example.com", "ftp://langfuse.example.com", "https://"} {
		handle, err := NewLangfuseTraceHandler(LangfuseConfig{
			Host:      host,
			PublicKey: "pk-lf-test",
			SecretKey: "sk-lf-test",
		})
		if err == nil {
			t.Fatalf("host %q accepted (handle: %v)", host, handle)
		}
		if handle != nil {
			t.Fatalf("host %q returned handle %v, want nil", host, handle)
		}
	}
}

func TestNewLangfuseTraceHandlerInvalidSampleRate(t *testing.T) {
	for _, rate := range []float64{-0.5, 1.5, 42} {
		handle, err := NewLangfuseTraceHandler(LangfuseConfig{
			Host:       "https://langfuse.example.com",
			PublicKey:  "pk-lf-test",
			SecretKey:  "sk-lf-test",
			SampleRate: rate,
		})
		if err == nil {
			t.Fatalf("sample rate %g accepted (handle: %v)", rate, handle)
		}
		if !strings.Contains(err.Error(), "ONCLAW_LANGFUSE_SAMPLE_RATE") {
			t.Fatalf("error %q does not name ONCLAW_LANGFUSE_SAMPLE_RATE", err)
		}
		if handle != nil {
			t.Fatalf("sample rate %g returned handle %v, want nil", rate, handle)
		}
	}
}

func TestNewLangfuseTraceHandlerConfigured(t *testing.T) {
	// Construction is local: the export client queues events in background
	// workers and contacts the network only when events exist, so building
	// and flushing an unused handler is network-free.
	handle, err := NewLangfuseTraceHandler(LangfuseConfig{
		Host:       "https://langfuse.example.com/",
		PublicKey:  "pk-lf-test",
		SecretKey:  "sk-lf-test",
		SampleRate: 0.5,
	})
	if err != nil {
		t.Fatalf("configured construction failed: %v", err)
	}
	if handle == nil {
		t.Fatal("configured construction returned nil handle")
	}
	if handle.Callback() == nil {
		t.Fatal("Callback() is nil, want the eino callback handler")
	}
	if got := handle.Host(); got != "https://langfuse.example.com" {
		t.Fatalf("Host() = %q, want trailing slash trimmed", got)
	}
	handle.Flush() // must be safe with nothing queued
}

func TestNewLangfuseTraceHandlerDefaultSampleRate(t *testing.T) {
	// A zero rate means "unset" and defaults to exporting everything (D5).
	handle, err := NewLangfuseTraceHandler(LangfuseConfig{
		Host:       "https://langfuse.example.com",
		PublicKey:  "pk-lf-test",
		SecretKey:  "sk-lf-test",
		SampleRate: 0,
	})
	if err != nil {
		t.Fatalf("zero sample rate rejected: %v", err)
	}
	if handle == nil {
		t.Fatal("zero sample rate returned nil handle")
	}
	handle.Flush()
}

func TestTraceURL(t *testing.T) {
	cases := []struct {
		host, traceID, want string
	}{
		{"https://langfuse.example.com", "abc-123", "https://langfuse.example.com/trace/abc-123"},
		{"https://langfuse.example.com/", "abc-123", "https://langfuse.example.com/trace/abc-123"},
		{"", "abc-123", ""},
		{"https://langfuse.example.com", "", ""},
	}
	for _, tc := range cases {
		if got := TraceURL(tc.host, tc.traceID); got != tc.want {
			t.Fatalf("TraceURL(%q, %q) = %q, want %q", tc.host, tc.traceID, got, tc.want)
		}
	}
}

func TestLangfuseConfigEmpty(t *testing.T) {
	if !(LangfuseConfig{}).empty() {
		t.Fatal("zero config must be empty")
	}
	if !(LangfuseConfig{SampleRate: 0.5}).empty() {
		t.Fatal("rate alone must not make the config present")
	}
	if (LangfuseConfig{Host: "https://lf"}).empty() {
		t.Fatal("host alone must make the config present")
	}
	if (LangfuseConfig{PublicKey: "pk"}).empty() {
		t.Fatal("public key alone must make the config present")
	}
	if (LangfuseConfig{SecretKey: "sk"}).empty() {
		t.Fatal("secret key alone must make the config present")
	}
}

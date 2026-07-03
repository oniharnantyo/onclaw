package web

import (
	"errors"
	"fmt"
	"net"
	"net/url"
)

// AllowLoopbackForTest can be set to true in test suites to bypass loopback IP blocking.
var AllowLoopbackForTest = false

// ValidateURLNotInternal parses the URL and ensures it does not resolve to an internal/private address.
func ValidateURLNotInternal(urlStr string) error {
	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %q", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return errors.New("empty host in URL")
	}

	// If host is directly an IP address, validate it
	if ip := net.ParseIP(host); ip != nil {
		if err := validateIP(ip); err != nil {
			return err
		}
		return nil
	}

	// Resolve hostname
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("failed to resolve host %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("no IP addresses found for host %q", host)
	}

	for _, ip := range ips {
		if err := validateIP(ip); err != nil {
			return err
		}
	}

	return nil
}

func validateIP(ip net.IP) error {
	if ip.IsLoopback() {
		if AllowLoopbackForTest {
			return nil
		}
		return fmt.Errorf("blocked loopback address: %s", ip)
	}
	if ip.IsPrivate() {
		return fmt.Errorf("blocked private network address: %s", ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("blocked link-local address: %s", ip)
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("blocked unspecified address: %s", ip)
	}
	// Extra metadata block
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return fmt.Errorf("blocked cloud metadata endpoint: %s", ip)
	}
	return nil
}

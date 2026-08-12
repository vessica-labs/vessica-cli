package newsletter

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// NewPublicHTTPClient blocks private, loopback, link-local, multicast, and
// credential-bearing redirect targets. The resolved public address is dialed
// directly to avoid DNS rebinding between validation and connection.
func NewPublicHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("resolve public source: %w", err)
		}
		addresses, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("resolve public source: %w", err)
		}
		for _, addressIP := range addresses {
			if publicIP(addressIP) {
				return dialer.DialContext(ctx, network, net.JoinHostPort(addressIP.String(), port))
			}
		}
		return nil, fmt.Errorf("public source resolved only to private or reserved addresses")
	}
	return &http.Client{
		Transport: transport,
		Timeout:   20 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("public source redirect limit exceeded")
			}
			if request.URL.Scheme != "https" || request.URL.User != nil || request.URL.Hostname() == "" {
				return fmt.Errorf("public source redirect must remain credential-free HTTPS")
			}
			return nil
		},
	}
}

func publicIP(address net.IP) bool {
	return address != nil && !address.IsPrivate() && !address.IsLoopback() && !address.IsLinkLocalUnicast() && !address.IsLinkLocalMulticast() && !address.IsUnspecified() && !address.IsMulticast()
}

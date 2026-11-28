package middleware

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/businessos/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
)

// RequireIPAllowlist restricts a route to the configured IPs/CIDRs. An empty
// allowlist disables the check so deployments that have not configured it keep
// working; the callback token and HMAC still apply. A nonempty allowlist that
// contains an invalid entry fails closed instead of silently becoming
// unrestricted.
func RequireIPAllowlist(entries []string) gin.HandlerFunc {
	prefixes, parseErr := parsePrefixes(entries)
	return func(c *gin.Context) {
		if parseErr != nil {
			response.Error(c, 403, "callback source not allowed")
			c.Abort()
			return
		}
		if len(prefixes) == 0 {
			c.Next()
			return
		}
		addr, err := netip.ParseAddr(c.ClientIP())
		if err != nil {
			response.Error(c, 403, "callback source not allowed")
			c.Abort()
			return
		}
		for _, prefix := range prefixes {
			if prefix.Contains(addr) {
				c.Next()
				return
			}
		}
		response.Error(c, 403, "callback source not allowed")
		c.Abort()
	}
}

func parsePrefixes(entries []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if !strings.Contains(entry, "/") {
			if addr, err := netip.ParseAddr(entry); err == nil {
				prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
				continue
			}
			return nil, fmt.Errorf("invalid IP allowlist entry %q", entry)
		}
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid IP allowlist entry %q: %w", entry, err)
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

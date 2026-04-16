package middleware

import (
	"net"
	"net/http"

	"github.com/go-logr/logr"
)

func RequireTrustedSourceMiddleware(
	log logr.Logger,
	trustedSourceCIDRs []*net.IPNet,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isFromTrustedSource(r.RemoteAddr, trustedSourceCIDRs) {
				log.Info(
					"rejecting request from untrusted source",
					"remoteAddr", r.RemoteAddr,
				)

				http.Error(w, "request source is not allowed", http.StatusForbidden)

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isFromTrustedSource(remoteAddr string, trustedSourceCIDRs []*net.IPNet) bool {
	if len(trustedSourceCIDRs) == 0 {
		return true
	}

	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	for _, cidr := range trustedSourceCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}

	return false
}

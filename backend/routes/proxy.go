package routes

import (
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"time"

	"voltis/models"

	"github.com/labstack/echo/v4"
)

// Only the TCP peer decides trust: RealIP() believes X-Forwarded-For.
func (r *resolver) proxyIdentity(c echo.Context) (ExternalIdentity, bool, error) {
	if !r.proxy.Enabled() {
		return ExternalIdentity{}, false, nil
	}
	header := c.Request().Header
	values := header.Values(r.proxy.UserHeader)
	if len(values) == 0 {
		return ExternalIdentity{}, false, nil
	}
	if !r.trustedPeer(c) {
		warnUntrustedPeer(c.Request().RemoteAddr, r.proxy.UserHeader)
		return ExternalIdentity{}, false, nil
	}
	if len(values) > 1 {
		return ExternalIdentity{}, false, echo.NewHTTPError(http.StatusBadRequest,
			"the "+r.proxy.UserHeader+" header was sent more than once")
	}

	name := strings.TrimSpace(values[0])
	if name == "" {
		return ExternalIdentity{}, false, echo.NewHTTPError(http.StatusBadRequest,
			"the "+r.proxy.UserHeader+" header is empty")
	}

	id := ExternalIdentity{
		Provider: models.SessionProxy,
		Subject:  name,
		Username: name,
	}
	if r.proxy.EmailHeader != "" {
		id.Email = strings.TrimSpace(header.Get(r.proxy.EmailHeader))
	}
	if r.proxy.GroupsHeader != "" {
		if raw, ok := header[http.CanonicalHeaderKey(r.proxy.GroupsHeader)]; ok {
			id.Groups, id.HasGroups = parseGroups(strings.Join(raw, ","))
		}
	}
	return id, true, nil
}

func (r *resolver) trustedPeer(c echo.Context) bool {
	host, _, err := net.SplitHostPort(c.Request().RemoteAddr)
	if err != nil {
		host = c.Request().RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	for _, cidr := range r.proxy.TrustedCIDRs {
		if cidr.Contains(addr) {
			return true
		}
	}
	return false
}

var lastUntrustedWarning atomic.Int64

func warnUntrustedPeer(peer, header string) {
	now, last := time.Now().Unix(), lastUntrustedWarning.Load()
	if now-last < 60 || !lastUntrustedWarning.CompareAndSwap(last, now) {
		return
	}
	slog.Warn("[auth] ignoring a forwarded-auth header from an untrusted peer", "peer", peer, "header", header)
}

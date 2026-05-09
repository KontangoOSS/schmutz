package main

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/KontangoOSS/schmutz/controller/internal/service"
)

// zitiEdgeConn is the interface for edge connections that carry Ziti identity.
// Satisfied by the SDK's edge.Conn when connecting via a Ziti listener.
type zitiEdgeConn interface {
	net.Conn
	GetDialerIdentityName() string
	GetDialerIdentityId() string
}

// Context keys for Ziti identity injected by ConnContext on overlay listeners.
type zitiIdentityKey struct{}
type zitiIdentityIDKey struct{}

// zitiCallerName extracts the Ziti identity name of the connecting client from context.
func zitiCallerName(ctx context.Context) string {
	v, _ := ctx.Value(zitiIdentityKey{}).(string)
	return v
}

func zitiCallerID(ctx context.Context) string {
	v, _ := ctx.Value(zitiIdentityIDKey{}).(string)
	return v
}

// appetizer holds server-side truth from Caddy headers at request time.
type appetizer struct {
	JA4        string
	RealIP     string
	TLSVersion string
	Timestamp  int64
}

// captureAppetizer reads Caddy-injected headers from the incoming request.
func captureAppetizer(r *http.Request) appetizer {
	ip := r.Header.Get("X-Real-Ip")
	if ip == "" {
		ip = r.Header.Get("X-Real-IP")
	}
	if ip == "" {
		ip = r.Header.Get("X-Forwarded-For")
	}
	if ip == "" {
		ip = r.RemoteAddr
	}
	ja4 := r.Header.Get("X-Ja4")
	if ja4 == "" {
		ja4 = r.Header.Get("X-JA4")
	}
	return appetizer{
		JA4:        ja4,
		RealIP:     ip,
		TLSVersion: r.Header.Get("X-TLS-Version"),
		Timestamp:  time.Now().UnixMilli(),
	}
}

// loadOSCatalog loads the full OS catalog from Bao.
func loadOSCatalog(store *service.StoreService) []map[string]interface{} {
	families, _ := store.List("public", "os/")
	var out []map[string]interface{}
	for _, f := range families {
		f = strings.TrimSuffix(f, "/")
		if f == "" {
			continue
		}
		distros, _ := store.List("public", "os/"+f+"/")
		var dists []map[string]interface{}
		for _, d := range distros {
			d = strings.TrimSuffix(d, "/")
			if d == "" {
				continue
			}
			versions, _ := store.List("public", "os/"+f+"/"+d+"/")
			var vers []map[string]string
			for _, v := range versions {
				v = strings.TrimSuffix(v, "/")
				if v == "" {
					continue
				}
				entry := map[string]string{"version": v, "ref": "public/os/" + f + "/" + d + "/" + v}
				if data, _ := store.Get("public", "os/"+f+"/"+d+"/"+v); data != nil {
					if cn, ok := data["codename"].(string); ok {
						entry["codename"] = cn
					}
				}
				vers = append(vers, entry)
			}
			if len(vers) > 0 {
				dists = append(dists, map[string]interface{}{"name": d, "versions": vers})
			}
		}
		if len(dists) > 0 {
			out = append(out, map[string]interface{}{"family": f, "distros": dists})
		}
	}
	return out
}

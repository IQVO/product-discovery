package http

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// readCacheControl is the modest, CDN-friendly freshness window carried by
// every product and category read: the projection is eventually consistent
// with the source contexts anyway, so half a minute of shared caching trades
// no correctness for a large cut in origin traffic.
const readCacheControl = "max-age=30"

// entityTag is the strong validator of a read: the quoted hex of the first
// 16 bytes of the representation's SHA-256. It is computed from the exact
// bytes served, so anything that changes the representation — a new content
// revision, a price version bump, a listing flip, a category rename —
// changes the tag.
func entityTag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}

// notModified reports whether the request's If-None-Match matches etag
// (RFC 9110 §13.1.2.): "*" matches any current representation, and the
// comparison is weak, so a W/ prefix on a candidate tag is ignored.
func notModified(r *http.Request, etag string) bool {
	inm := strings.TrimSpace(r.Header.Get("If-None-Match"))
	if inm == "" {
		return false
	}
	if inm == "*" {
		return true
	}
	for _, cand := range strings.Split(inm, ",") {
		if strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cand), "W/")) == etag {
			return true
		}
	}
	return false
}

// writeCacheableJSON writes v as a cacheable read: ETag and Cache-Control on
// the response, and 304 without a body when If-None-Match matches. The body
// is marshaled once and serves both the tag and the response, so the tag can
// never disagree with what is served.
func writeCacheableJSON(w http.ResponseWriter, r *http.Request, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		writeError(w, r, err)
		return
	}
	etag := entityTag(body)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", readCacheControl)
	if notModified(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(body, '\n'))
}

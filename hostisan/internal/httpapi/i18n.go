package httpapi

import (
	"errors"
	"net/http"

	"github.com/hostisan/hostisan/internal/brand"
	"github.com/hostisan/hostisan/internal/i18n"
)

// translations serves one language's catalog. It needs no session: the login page
// is translated too.
func (s *server) translations(w http.ResponseWriter, r *http.Request) {
	msgs, err := s.catalogs.Messages(r.PathValue("lang"))
	if errors.Is(err, i18n.ErrUnknownLanguage) {
		WriteError(w, http.StatusNotFound, "not_found", "")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	out := make(map[string]string, len(msgs))
	for id, tr := range msgs {
		out[id] = brand.Rewrite(tr)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	writeJSONHeadersSet(w, out)
}

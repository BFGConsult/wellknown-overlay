package overlay

import (
	"errors"
	"io"
	"net/http"
)

const HealthPath = "/healthz"

func NewHTTPHandler(responder *Responder) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !(r.Method == http.MethodPost && isAutodiscoverPath(r.URL.Path)) {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if r.URL.Path == HealthPath {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			if r.Method == http.MethodHead {
				return
			}
			_, _ = w.Write([]byte("ok\n"))
			return
		}

		var body []byte
		if r.Method == http.MethodPost {
			var err error
			body, err = io.ReadAll(io.LimitReader(r.Body, 64*1024))
			if err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
		}

		response, err := responder.RenderRequestBody(r.URL.Path, r.URL.RawQuery, body)
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", response.ContentType)
		w.WriteHeader(response.Status)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(response.Body)
	})
}

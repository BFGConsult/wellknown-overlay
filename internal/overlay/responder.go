package overlay

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"path"
)

var ErrNotFound = errors.New("route not found")

type Response struct {
	Status      int
	ContentType string
	Body        []byte
}

type Responder struct {
	routes          map[string]Route
	emailAutoconfig *EmailAutoconfig
	files           fs.FS
}

func NewResponder(cfg Config, files fs.FS) *Responder {
	routes := make(map[string]Route, len(cfg.Routes))
	for _, route := range cfg.Routes {
		routes[route.Path] = route
	}

	return &Responder{
		routes:          routes,
		emailAutoconfig: cfg.EmailAutoconfig,
		files:           files,
	}
}

func (r *Responder) Render(routePath string) (Response, error) {
	if r.emailAutoconfig != nil && isEmailAutoconfigPath(routePath) {
		body, err := RenderEmailAutoconfig(*r.emailAutoconfig)
		if err != nil {
			return Response{}, err
		}
		return Response{
			Status:      http.StatusOK,
			ContentType: "application/xml",
			Body:        body,
		}, nil
	}

	route, ok := r.routes[routePath]
	if !ok {
		return Response{Status: http.StatusNotFound}, ErrNotFound
	}

	body, err := fs.ReadFile(r.files, route.File)
	if err != nil {
		return Response{}, err
	}

	contentType := route.ContentType
	if contentType == "" {
		contentType = mime.TypeByExtension(path.Ext(route.File))
	}
	if contentType == "" {
		contentType = http.DetectContentType(body)
	}

	return Response{
		Status:      http.StatusOK,
		ContentType: contentType,
		Body:        body,
	}, nil
}

func isEmailAutoconfigPath(routePath string) bool {
	for _, path := range EmailAutoconfigPaths() {
		if routePath == path {
			return true
		}
	}
	return false
}

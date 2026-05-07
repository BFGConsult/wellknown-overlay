package overlay

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
)

var ErrNotFound = errors.New("route not found")

type Response struct {
	Status      int
	ContentType string
	Body        []byte
}

type Responder struct {
	routes      map[string]Route
	mailAccount *MailAccount
	files       fs.FS
}

func NewResponder(cfg Config, files fs.FS) *Responder {
	routes := make(map[string]Route, len(cfg.Routes))
	for _, route := range cfg.Routes {
		routes[route.Path] = route
	}

	return &Responder{
		routes:      routes,
		mailAccount: cfg.MailAccount,
		files:       files,
	}
}

func (r *Responder) Render(routePath string) (Response, error) {
	return r.RenderRequest(routePath, nil)
}

func (r *Responder) RenderRequest(routePath string, query url.Values) (Response, error) {
	if r.mailAccount != nil && isThunderbirdAutoconfigPath(routePath) {
		body, err := RenderThunderbirdAutoconfig(*r.mailAccount)
		if err != nil {
			return Response{}, err
		}
		return Response{
			Status:      http.StatusOK,
			ContentType: "application/xml",
			Body:        body,
		}, nil
	}
	if r.mailAccount != nil && routePath == AppleMobileconfigPath {
		body, err := RenderAppleMobileconfig(*r.mailAccount, query.Get("emailaddress"))
		if err != nil {
			return Response{}, err
		}
		return Response{
			Status:      http.StatusOK,
			ContentType: "application/x-apple-aspen-config",
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

func isThunderbirdAutoconfigPath(routePath string) bool {
	for _, path := range ThunderbirdAutoconfigPaths() {
		if routePath == path {
			return true
		}
	}
	return false
}

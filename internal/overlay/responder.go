package overlay

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
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
		// Targeted routes are rendered directly by the gateway's nginx server
		// blocks. The standalone overlay server only exposes untargeted routes.
		if route.Target != "" {
			continue
		}
		routes[route.Path] = route
	}

	return &Responder{
		routes:      routes,
		mailAccount: cfg.MailAccount,
		files:       files,
	}
}

func (r *Responder) Render(routePath string) (Response, error) {
	return r.RenderRequest(routePath, "")
}

func (r *Responder) RenderRequest(routePath, rawQuery string) (Response, error) {
	return r.RenderRequestBody(routePath, rawQuery, nil)
}

func (r *Responder) RenderRequestBody(routePath, rawQuery string, body []byte) (Response, error) {
	return r.RenderHTTPRequest(routePath, rawQuery, body, "")
}

func (r *Responder) RenderHTTPRequest(routePath, rawQuery string, body []byte, publicBaseURL string) (Response, error) {
	return r.RenderHTTPRequestForHost(routePath, rawQuery, body, publicBaseURL, "")
}

func (r *Responder) RenderHTTPRequestForHost(routePath, rawQuery string, body []byte, publicBaseURL, requestHost string) (Response, error) {
	if r.mailAccount != nil && isThunderbirdAutoconfigPath(routePath) {
		emailAddress := queryValueAny(rawQuery, "emailaddress", "EmailAddress")
		profile := r.mailAccount.SelectProfileForRequest(emailAddress, requestHost)
		body, err := RenderThunderbirdAutoconfig(profile, emailAddress, r.mailAccount.ManualSetupURL())
		if err != nil {
			return Response{}, err
		}
		return Response{
			Status:      http.StatusOK,
			ContentType: "application/xml",
			Body:        body,
		}, nil
	}
	if r.mailAccount != nil && routePath == MailSetupPath {
		emailAddress := queryValueAny(rawQuery, "emailaddress", "EmailAddress")
		lang := queryValueAny(rawQuery, "lang", "locale")
		profile := r.mailAccount.SelectProfileForRequest(emailAddress, requestHost)
		imageURL := ""
		if r.mailAccount.SharePreviewEnabled() {
			imageURL = mailSetupImageURL(publicBaseURL, lang)
		}
		body, _, err := RenderMailSetup(r.files, profile, r.mailAccount.ManualSetup, emailAddress, lang, imageURL)
		if err != nil {
			return Response{}, err
		}
		return Response{
			Status:      http.StatusOK,
			ContentType: "text/html; charset=utf-8",
			Body:        body,
		}, nil
	}
	if r.mailAccount != nil && routePath == MailSetupImagePath {
		if !r.mailAccount.SharePreviewEnabled() {
			return Response{Status: http.StatusNotFound}, ErrNotFound
		}
		body, err := RenderMailSetupSharePreviewImage()
		if err != nil {
			return Response{}, err
		}
		return Response{
			Status:      http.StatusOK,
			ContentType: "image/png",
			Body:        body,
		}, nil
	}
	if r.mailAccount != nil && routePath == AppleMobileconfigPath {
		emailAddress := queryValueAny(rawQuery, "emailaddress", "EmailAddress")
		profile := r.mailAccount.SelectProfileForRequest(emailAddress, requestHost)
		body, err := RenderAppleMobileconfig(profile, emailAddress)
		if err != nil {
			return Response{}, err
		}
		return Response{
			Status:      http.StatusOK,
			ContentType: "application/x-apple-aspen-config",
			Body:        body,
		}, nil
	}
	if r.mailAccount != nil && isAutodiscoverPath(routePath) {
		emailAddress := queryValueAny(rawQuery, "emailaddress", "EmailAddress")
		if emailAddress == "" {
			emailAddress = EmailAddressFromAutodiscoverRequest(body)
		}
		profile := r.mailAccount.SelectProfileForRequest(emailAddress, requestHost)
		body, err := RenderAutodiscover(profile, emailAddress)
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

func mailSetupImageURL(publicBaseURL, lang string) string {
	if publicBaseURL == "" {
		return MailSetupImagePath
	}
	query := url.Values{}
	query.Set("lang", normalizeLanguage(lang))
	return publicBaseURL + MailSetupImagePath + "?" + query.Encode()
}

func isThunderbirdAutoconfigPath(routePath string) bool {
	for _, path := range ThunderbirdAutoconfigPaths() {
		if routePath == path {
			return true
		}
	}
	return false
}

func isAutodiscoverPath(routePath string) bool {
	for _, path := range AutodiscoverPaths() {
		if routePath == path {
			return true
		}
	}
	return false
}

func queryValueAny(rawQuery string, keys ...string) string {
	for _, key := range keys {
		if value := queryValue(rawQuery, key); value != "" {
			return value
		}
	}
	return ""
}

func queryValue(rawQuery, key string) string {
	for _, part := range strings.Split(rawQuery, "&") {
		name, value, ok := strings.Cut(part, "=")
		if !ok || name != key {
			continue
		}
		unescaped, err := url.PathUnescape(value)
		if err != nil {
			return value
		}
		return unescaped
	}
	return ""
}

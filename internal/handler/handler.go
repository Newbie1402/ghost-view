package handler

import (
	"ghostview/internal/config"
	"ghostview/internal/downloader"
	"ghostview/internal/httputil"
	"ghostview/internal/middleware"
	"ghostview/internal/model"
	"ghostview/internal/service"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
)

type Handler struct {
	service    *service.Service
	downloader *downloader.Downloader
	config     config.Config
}

func New(s *service.Service, d *downloader.Downloader, c config.Config, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{s, d, c}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", h.health)
	mux.HandleFunc("GET /api/v1/providers", h.providers)
	mux.HandleFunc("GET /api/v1/search", h.search)
	mux.HandleFunc("GET /api/v1/profiles/{platform}/{username}", h.profile)
	mux.HandleFunc("GET /api/v1/profiles/{platform}/{username}/posts", h.posts)
	mux.HandleFunc("GET /api/v1/profiles/{platform}/{username}/reposts", h.reposts)
	mux.HandleFunc("GET /api/v1/profiles/{platform}/{username}/stories", h.stories)
	mux.HandleFunc("GET /api/v1/profiles/{platform}/{username}/highlights", h.highlights)
	mux.HandleFunc("GET /api/v1/media/{platform}/{mediaId}/download", h.download)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		httputil.Fail(w, httputil.NewError("NOT_FOUND", "Endpoint was not found.", 404))
	})
	mux.HandleFunc("/", h.static)
	return middleware.Observe(logger, middleware.Security(middleware.Limits(middleware.NewLimiter(c.RateLimitPerMinute).Wrap(middleware.CORS(c.AllowedOrigins, mux)))))
}
func validQuery(w http.ResponseWriter, r *http.Request, keys ...string) bool {
	q, err := url.ParseQuery(r.URL.RawQuery)
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	for k, v := range q {
		if !allowed[k] || len(v) != 1 {
			err = httputil.Invalid
		}
	}
	if err != nil || strings.Contains(r.URL.RawQuery, ";") {
		httputil.Fail(w, httputil.Invalid)
		return false
	}
	return true
}
func platform(r *http.Request) model.Platform { return model.Platform(r.PathValue("platform")) }
func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	if !validQuery(w, r) {
		return
	}
	httputil.JSON(w, 200, map[string]string{"status": "ok", "mode": h.config.Mode})
}
func (h *Handler) providers(w http.ResponseWriter, r *http.Request) {
	if !validQuery(w, r) {
		return
	}
	httputil.JSON(w, 200, h.service.Providers())
}
func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	if !validQuery(w, r, "platform", "q") {
		return
	}
	data, e := h.service.Search(r.Context(), model.Platform(r.URL.Query().Get("platform")), r.URL.Query().Get("q"))
	if e != nil {
		httputil.Fail(w, e)
		return
	}
	httputil.JSON(w, 200, data)
}
func (h *Handler) profile(w http.ResponseWriter, r *http.Request) {
	if !validQuery(w, r) {
		return
	}
	data, e := h.service.Profile(r.Context(), platform(r), r.PathValue("username"))
	if e != nil {
		httputil.Fail(w, e)
		return
	}
	httputil.JSON(w, 200, data)
}
func (h *Handler) posts(w http.ResponseWriter, r *http.Request) {
	if !validQuery(w, r, "cursor") {
		return
	}
	data, e := h.service.Posts(r.Context(), platform(r), r.PathValue("username"), r.URL.Query().Get("cursor"))
	if e != nil {
		httputil.Fail(w, e)
		return
	}
	httputil.JSON(w, 200, data)
}
func (h *Handler) reposts(w http.ResponseWriter, r *http.Request) {
	if !validQuery(w, r, "cursor") {
		return
	}
	data, e := h.service.Reposts(r.Context(), platform(r), r.PathValue("username"), r.URL.Query().Get("cursor"))
	if e != nil {
		httputil.Fail(w, e)
		return
	}
	httputil.JSON(w, 200, data)
}
func (h *Handler) stories(w http.ResponseWriter, r *http.Request) {
	if !validQuery(w, r) {
		return
	}
	data, e := h.service.Stories(r.Context(), platform(r), r.PathValue("username"))
	if e != nil {
		httputil.Fail(w, e)
		return
	}
	httputil.JSON(w, 200, data)
}
func (h *Handler) highlights(w http.ResponseWriter, r *http.Request) {
	if !validQuery(w, r) {
		return
	}
	data, e := h.service.Highlights(r.Context(), platform(r), r.PathValue("username"))
	if e != nil {
		httputil.Fail(w, e)
		return
	}
	httputil.JSON(w, 200, data)
}
func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	if !validQuery(w, r) {
		return
	}
	resource, e := h.service.Download(r.Context(), platform(r), r.PathValue("mediaId"))
	if e != nil {
		httputil.Fail(w, e)
		return
	}
	result, e := h.downloader.Fetch(r.Context(), resource)
	if e != nil {
		httputil.Fail(w, e)
		return
	}
	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("Content-Disposition", result.Disposition())
	w.Header().Set("Content-Length", result.ContentLength())
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Bytes)
}
func (h *Handler) static(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path
	if name == "/" {
		name = "/index.html"
	}
	if strings.ContainsAny(name, "\\\x00") || path.Clean(name) != name || (name != "/index.html" && !strings.HasPrefix(name, "/assets/")) {
		http.NotFound(w, r)
		return
	}
	root, e := os.OpenRoot(h.config.WebDir)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	f, e := root.Open(strings.TrimPrefix(name, "/"))
	if e != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	stat, e := f.Stat()
	if e != nil || !stat.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}

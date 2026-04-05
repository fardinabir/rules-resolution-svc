package server

import (
	"embed"
	"io/fs"
	"net/http"

	httpSwagger "github.com/swaggo/http-swagger/v2"
)

//go:embed openapi.yaml
var specFS embed.FS

// NewSwaggerHandler returns an http.Handler that serves:
//   - GET /swagger/*   → Swagger UI (via swaggo/http-swagger)
//   - GET /openapi.yaml → raw OpenAPI spec
func NewSwaggerHandler() http.Handler {
	mux := http.NewServeMux()

	// Serve raw spec — http-swagger will load it via the ?url= parameter
	mux.HandleFunc("/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		data, err := fs.ReadFile(specFS, "openapi.yaml")
		if err != nil {
			http.Error(w, "spec unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(data)
	})

	// Swagger UI — points to the locally served spec
	mux.Handle("/swagger/", httpSwagger.Handler(
		httpSwagger.URL("/openapi.yaml"),
		httpSwagger.DeepLinking(true),
		httpSwagger.DocExpansion("list"),
		httpSwagger.DomID("swagger-ui"),
	))

	// Redirect root to Swagger UI
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/swagger/index.html", http.StatusMovedPermanently)
	})

	return mux
}

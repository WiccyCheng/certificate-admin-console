package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"certadmin/internal/httpapi"
	"certadmin/internal/service"
	"certadmin/internal/store"

	"github.com/gin-gonic/gin"
)

func main() {
	dbPath := flag.String("db", "data/demo.db", "SQLite database path")
	addr := flag.String("addr", ":8080", "HTTP listen address")
	webDir := flag.String("web-dir", "web/dist", "built frontend directory")
	flag.Parse()
	gin.SetMode(gin.ReleaseMode)

	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	seeded, err := db.SeedIfEmpty()
	if err != nil {
		log.Fatalf("seed: %v", err)
	}
	if seeded {
		log.Printf("initialized demo data in %s", db.Path)
	}

	svc := service.New(db)
	srv := httpapi.New(svc)
	mux := http.NewServeMux()
	mux.Handle("/api", srv.Handler())
	mux.Handle("/api/", srv.Handler())
	mux.Handle("/", spaHandler(*webDir))

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("certificate admin listening on %s (db=%s)", *addr, db.Path)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

// spaHandler serves the built frontend and falls back to index.html for
// non-API paths. API paths are routed separately and keep JSON 404s.
func spaHandler(dir string) http.Handler {
	root := http.Dir(dir)
	files := http.FileServer(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		if clean == "/" {
			serveIndex(w, r, dir)
			return
		}
		if info, err := root.Open(clean); err == nil {
			info.Close()
			files.ServeHTTP(w, r)
			return
		}
		serveIndex(w, r, dir)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, dir string) {
	index := filepath.Join(dir, "index.html")
	if _, err := os.Stat(index); err != nil {
		http.Error(w, "frontend not built: run `npm --prefix web install && npm --prefix web run build`", http.StatusServiceUnavailable)
		return
	}
	http.ServeFile(w, r, index)
}

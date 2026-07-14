package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

type app struct {
	db        *mongo.Database
	token     string
	publicURL string
	reqLog    requestLog
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	uri := getenv("MONGO_URI", "mongodb://localhost:27017")
	dbName := getenv("MONGO_DB", "mongocp")
	token := os.Getenv("API_TOKEN")
	if token == "" {
		log.Fatal("API_TOKEN environment variable is required")
	}
	port := getenv("PORT", "8080")

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatalf("mongo connect: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		log.Fatalf("mongo ping: %v", err)
	}
	log.Printf("connected to MongoDB, using database %q", dbName)

	a := &app{
		db:        client.Database(dbName),
		token:     token,
		publicURL: os.Getenv("PUBLIC_URL"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.handleHealth)
	mux.HandleFunc("GET /openapi.json", a.handleOpenAPI)
	// All mutating endpoints are flat POSTs with the collection in the JSON
	// body: the GPT Actions importer reliably picks up body-only schemas but
	// tends to drop request bodies on operations that mix path parameters
	// with a body.
	mux.Handle("GET /collections", a.auth(a.handleListCollections))
	mux.Handle("POST /collections/create", a.auth(a.handleCreateCollection))
	mux.Handle("POST /collections/drop", a.auth(a.handleDropCollection))
	mux.Handle("POST /documents/insert", a.auth(a.handleInsert))
	mux.Handle("POST /documents/query", a.auth(a.handleQuery))
	mux.Handle("POST /documents/update", a.auth(a.handleUpdate))
	mux.Handle("POST /documents/delete", a.auth(a.handleDelete))
	mux.Handle("POST /documents/aggregate", a.auth(a.handleAggregate))
	mux.Handle("GET /debug/requests", a.auth(a.handleDebugRequests))

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           limitBody(a.logRequests(mux)),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
	}

	go func() {
		log.Printf("listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Print("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	_ = client.Disconnect(shutdownCtx)
}

// auth requires a valid "Authorization: Bearer <API_TOKEN>" header.
func (a *app) auth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if len(header) <= len(prefix) ||
			!strings.EqualFold(header[:len(prefix)], prefix) ||
			subtle.ConstantTimeCompare([]byte(header[len(prefix):]), []byte(a.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
			return
		}
		next(w, r)
	})
}

// limitBody caps request bodies at 5 MiB.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
		next.ServeHTTP(w, r)
	})
}

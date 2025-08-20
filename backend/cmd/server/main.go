package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

// ================= Local Cache (TTL) =================

type item struct {
	val     []byte
	expires time.Time
}

type ttlCache struct {
	mu   sync.RWMutex
	data map[string]item
	ttl  time.Duration
}

func newTTLCache(ctx context.Context, ttl time.Duration) *ttlCache {
	c := &ttlCache{data: make(map[string]item), ttl: ttl}

	// limpeza periódica controlada por contexto
	go func() {
		t := time.NewTicker(ttl)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				now := time.Now()
				c.mu.Lock()
				for k, it := range c.data {
					if now.After(it.expires) {
						delete(c.data, k)
					}
				}
				c.mu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()
	return c
}

func (c *ttlCache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	it, ok := c.data[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(it.expires) {
		return nil, false
	}
	return it.val, true
}

func (c *ttlCache) Set(key string, val []byte) {
	c.mu.Lock()
	c.data[key] = item{val: val, expires: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

// ================= App =================

const tenantHeader = "X-Tenant-Id"

func shardKeyFrom(r *http.Request) (string, error) {
	v := r.Header.Get(tenantHeader)
	if v == "" {
		return "", errors.New("missing X-Tenant-Id")
	}
	return v, nil
}

// key namespace: tenant + ":" + appKey
func namespacedKey(tenant, key string) string {
	return tenant + ":" + key
}

func sha1Hex(b []byte) string {
	s := sha1.Sum(b)
	return hex.EncodeToString(s[:])
}

func main() {
	// contexto raiz + shutdown gracioso
	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// captura de sinais para shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	port := getenv("APP_PORT", "8080")

	// Redis Options + tunings
	opt, err := redis.ParseURL(getenv("REDIS_URL", "redis://localhost:6379/0"))
	if err != nil {
		log.Fatal(err)
	}
	// tunings recomendados (ajuste conforme o tráfego)
	opt.MaxRetries = 2
	opt.DialTimeout = 500 * time.Millisecond
	opt.ReadTimeout = 300 * time.Millisecond
	opt.WriteTimeout = 300 * time.Millisecond
	opt.PoolSize = 50
	opt.MinIdleConns = 10
	opt.PoolTimeout = 500 * time.Millisecond

	rdb := redis.NewClient(opt)

	// Local cache (por nó). Afinidade do NGINX ajuda o hit-rate.
	local := newTTLCache(rootCtx, 30*time.Second)

	r := chi.NewRouter()

	// health
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})

	// GET /kv?key=foo -> retorna valor; se não existir, gera e persiste
	r.Get("/kv", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "missing key", 400)
			return
		}

		tenant, err := shardKeyFrom(r)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		nsKey := namespacedKey(tenant, key)

		// 1) tenta cache local
		if v, ok := local.Get(nsKey); ok {
			// deixa content-type por conta do cliente/uso; se preferir, use octet-stream:
			// w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(v)
			return
		}

		// 2) tenta Redis (com timeout por operação)
		opCtx, cancel := context.WithTimeout(r.Context(), 200*time.Millisecond)
		v, err := rdb.Get(opCtx, nsKey).Bytes()
		cancel()
		if err == nil {
			local.Set(nsKey, v)
			_, _ = w.Write(v)
			return
		}
		if err != redis.Nil { // erro real (não apenas key inexistente)
			log.Printf("warn: redis GET key=%s err=%v", nsKey, err)
		}

		// 3) simula DB: gera valor determinístico
		val := []byte("val-" + sha1Hex([]byte(nsKey)))

		// grava em Redis (com timeout) e local
		opCtx, cancel = context.WithTimeout(r.Context(), 200*time.Millisecond)
		if setErr := rdb.Set(opCtx, nsKey, val, 2*time.Minute).Err(); setErr != nil {
			log.Printf("warn: redis SET key=%s err=%v", nsKey, setErr)
		}
		cancel()
		local.Set(nsKey, val)

		_, _ = w.Write(val)
	})

	// POST /kv body: key=foo&val=bar -> seta valor
	r.Post("/kv", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		key := r.Form.Get("key")
		val := r.Form.Get("val")
		if key == "" {
			http.Error(w, "missing key", 400)
			return
		}

		tenant, err := shardKeyFrom(r)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		nsKey := namespacedKey(tenant, key)

		opCtx, cancel := context.WithTimeout(r.Context(), 200*time.Millisecond)
		if setErr := rdb.Set(opCtx, nsKey, val, 10*time.Minute).Err(); setErr != nil {
			log.Printf("warn: redis SET key=%s err=%v", nsKey, setErr)
		}
		cancel()
		local.Set(nsKey, []byte(val))
		w.WriteHeader(204)
	})

	// debug: mostra qual pod atendeu
	r.Get("/whoami", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		id := getenv("HOSTNAME", "unknown")
		_, _ = fmt.Fprintf(w, "server=%s\n", id)
	})

	// middleware: apenas headers padrão que não conflitam com handlers
	handler := withDefaultHeaders(r)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 2 * time.Second,
	}

	go func() {
		log.Printf("listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	// aguarda sinal e encerra
	select {
	case sig := <-sigCh:
		log.Printf("shutdown requested: %s", sig)
	case <-rootCtx.Done():
	}

	// shutdown gracioso do HTTP e cancel do rootCtx (encerra cleaner do cache)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}
	cancel() // encerra goroutines ligadas ao rootCtx
	log.Println("bye")
}

func withDefaultHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Não força Content-Type aqui; cada handler define o seu.
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func getenv(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

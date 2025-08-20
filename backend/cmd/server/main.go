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
	"strings"
	"sync"
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

func newTTLCache(ttl time.Duration) *ttlCache {
	c := &ttlCache{data: make(map[string]item), ttl: ttl}
	// limpeza periódica
	go func() {
		t := time.NewTicker(ttl)
		for range t.C {
			c.mu.Lock()
			for k, it := range c.data {
				if time.Now().After(it.expires) {
					delete(c.data, k)
				}
			}
			c.mu.Unlock()
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
	ctx := context.Background()
	port := getenv("APP_PORT", "8080")

	// Redis
	opt, err := redis.ParseURL(getenv("REDIS_URL", "redis://localhost:6379/0"))
	if err != nil {
		log.Fatal(err)
	}
	rdb := redis.NewClient(opt)

	// Local cache (por nó). Afinidade do NGINX ajuda o hit-rate.
	local := newTTLCache(30 * time.Second)

	r := chi.NewRouter()

	// health
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
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
			w.Write(v)
			return
		}

		// 2) tenta Redis
		v, err := rdb.Get(ctx, nsKey).Bytes()
		if err == nil {
			local.Set(nsKey, v)
			w.Write(v)
			return
		}

		// 3) simula DB: só gera um valor determinístico
		val := []byte("val-" + sha1Hex([]byte(nsKey)))

		// grava em Redis e local
		_ = rdb.Set(ctx, nsKey, val, 2*time.Minute).Err()
		local.Set(nsKey, val)

		w.Write(val)
	})
	// POST /kv body: key=foo&val=bar -> seta valor
	r.Post("/kv", func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
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
		_ = rdb.Set(ctx, nsKey, val, 10*time.Minute).Err()
		local.Set(nsKey, []byte(val))
		w.WriteHeader(204)
	})

	// debug: mostra qual pod atendeu
	r.Get("/whoami", func(w http.ResponseWriter, r *http.Request) {
		id := getenv("HOSTNAME", "unknown")
		fmt.Fprintf(w, "server=%s\n", id)
	})

	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, withDefaultHeaders(r)))
}

func withDefaultHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
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

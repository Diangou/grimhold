package open5e

import (
	"sync"
	"time"
)

// DefaultCacheTTL est la durée de vie par défaut d'une entrée du cache.
const DefaultCacheTTL = 5 * time.Minute

type cacheEntry struct {
	createdAt time.Time
	val       []byte
}

// Cache est un cache mémoire clé (URL) → corps de réponse, avec expiration.
// Une goroutine "reaper" purge périodiquement les entrées expirées ;
// Close l'arrête proprement.
type Cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	ttl     time.Duration
	now     func() time.Time

	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// CacheOption personnalise un Cache à la construction.
type CacheOption func(*Cache)

// WithClock remplace l'horloge du cache (utile pour les tests).
func WithClock(now func() time.Time) CacheOption {
	return func(c *Cache) {
		if now != nil {
			c.now = now
		}
	}
}

// NewCache crée un cache et démarre son reaper.
// ttl <= 0 utilise DefaultCacheTTL ; interval <= 0 utilise ttl/2.
func NewCache(ttl, interval time.Duration, opts ...CacheOption) *Cache {
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	if interval <= 0 {
		interval = ttl / 2
	}
	c := &Cache{
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
		now:     time.Now,
		done:    make(chan struct{}),
	}
	for _, opt := range opts {
		opt(c)
	}
	c.wg.Add(1)
	go c.reapLoop(interval)
	return c
}

// Add enregistre une copie de val sous la clé key.
func (c *Cache) Add(key string, val []byte) {
	cp := make([]byte, len(val))
	copy(cp, val)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{createdAt: c.now(), val: cp}
}

// Get renvoie une copie de la valeur associée à key. Une entrée expirée est
// considérée absente, même si le reaper ne l'a pas encore supprimée.
func (c *Cache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || c.expired(e, c.now()) {
		return nil, false
	}
	cp := make([]byte, len(e.val))
	copy(cp, e.val)
	return cp, true
}

// Len renvoie le nombre d'entrées stockées (expirées comprises tant que le
// reaper n'est pas passé).
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// Reap supprime immédiatement les entrées expirées. Le reaper l'appelle à
// chaque tick ; elle est exportée pour pouvoir être testée sans attendre.
func (c *Cache) Reap() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	for k, e := range c.entries {
		if c.expired(e, now) {
			delete(c.entries, k)
		}
	}
}

// Close arrête le reaper et attend sa fin. Appeler Close plusieurs fois est sans effet.
func (c *Cache) Close() {
	c.closeOnce.Do(func() { close(c.done) })
	c.wg.Wait()
}

func (c *Cache) reapLoop(interval time.Duration) {
	defer c.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.Reap()
		}
	}
}

// expired doit être appelée avec c.mu verrouillé.
func (c *Cache) expired(e cacheEntry, now time.Time) bool {
	return now.Sub(e.createdAt) >= c.ttl
}

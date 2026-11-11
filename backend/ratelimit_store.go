package main

// Shared storage for the Fiber rate limiters (login, change password).
//
// With REDIS_URL set, the counters live in Redis, so they survive an API restart and are shared if more
// than one API copy runs. If Redis is missing or fails, the limiter falls back to an in-memory map for
// that call: a Redis outage must never lock everybody out of logging in (it only makes the limit local).

import (
	"context"
	"log"
	"os"
	"strconv"
	"sync"
	"time"
)

const rateLimitPrefix = "leadloop:rl:"

// limitMemStore is a tiny in-memory fiber.Storage with expiry.
type limitMemStore struct {
	mu sync.Mutex
	m  map[string]limitItem
}

type limitItem struct {
	val []byte
	exp time.Time // zero = never
}

func newLimitMemStore() *limitMemStore { return &limitMemStore{m: map[string]limitItem{}} }

func (s *limitMemStore) Get(key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.m[key]
	if !ok {
		return nil, nil
	}
	if !it.exp.IsZero() && time.Now().After(it.exp) {
		delete(s.m, key)
		return nil, nil
	}
	return it.val, nil
}

func (s *limitMemStore) Set(key string, val []byte, exp time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := limitItem{val: append([]byte(nil), val...)}
	if exp > 0 {
		it.exp = time.Now().Add(exp)
	}
	s.m[key] = it
	if len(s.m) > 10000 { // sweep expired entries so the map cannot grow without bound
		now := time.Now()
		for k, v := range s.m {
			if !v.exp.IsZero() && now.After(v.exp) {
				delete(s.m, k)
			}
		}
	}
	return nil
}

func (s *limitMemStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

func (s *limitMemStore) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = map[string]limitItem{}
	return nil
}

func (s *limitMemStore) Close() error { return nil }

// redisStore implements fiber.Storage on the small Redis client in redisq.go, with the limitMemStore fallback.
type redisStore struct {
	c        *respClient
	fallback *limitMemStore
	warned   sync.Once
}

func (s *redisStore) fail(err error) {
	s.warned.Do(func() { log.Printf("rate limit: redis error (%v), using the in-memory limiter while it is down", err) })
}

func (s *redisStore) Get(key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	v, err := s.c.do(ctx, "GET", rateLimitPrefix+key)
	if err != nil {
		s.fail(err)
		return s.fallback.Get(key)
	}
	str, ok := v.(string)
	if !ok {
		return s.fallback.Get(key) // nil reply: not in redis (maybe written to the fallback during an outage)
	}
	return []byte(str), nil
}

func (s *redisStore) Set(key string, val []byte, exp time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	args := []string{"SET", rateLimitPrefix + key, string(val)}
	if exp > 0 {
		ms := exp.Milliseconds()
		if ms < 1 {
			ms = 1
		}
		args = append(args, "PX", strconv.FormatInt(ms, 10))
	}
	if _, err := s.c.do(ctx, args...); err != nil {
		s.fail(err)
		return s.fallback.Set(key, val, exp)
	}
	return nil
}

func (s *redisStore) Delete(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.fallback.Delete(key)
	if _, err := s.c.do(ctx, "DEL", rateLimitPrefix+key); err != nil {
		s.fail(err)
	}
	return nil
}

func (s *redisStore) Reset() error { return s.fallback.Reset() }
func (s *redisStore) Close() error { return nil }

// rateStorage has exactly the method set of fiber.Storage (kept local so this file and its test need no web framework).
type rateStorage interface {
	Get(key string) ([]byte, error)
	Set(key string, val []byte, exp time.Duration) error
	Delete(key string) error
	Reset() error
	Close() error
}

// newRateLimitStorage returns the shared limiter storage: Redis if REDIS_URL works, else memory.
func newRateLimitStorage() rateStorage {
	if u := os.Getenv("REDIS_URL"); u != "" {
		c, err := newRespClient(u)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err = c.do(ctx, "PING"); err == nil {
				log.Printf("rate limit storage: redis")
				return &redisStore{c: c, fallback: newLimitMemStore()}
			}
		}
		log.Printf("rate limit storage: redis unavailable (%v), using memory", err)
	}
	return newLimitMemStore()
}

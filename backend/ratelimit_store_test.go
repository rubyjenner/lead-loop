package main

import (
	"bufio"
	"context"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRedis is a minimal RESP server: PING, GET, SET key val [PX ms], DEL.
func fakeRedis(t *testing.T) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	data := map[string]string{}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					line, err := readLine(r)
					if err != nil || !strings.HasPrefix(line, "*") {
						return
					}
					n, _ := strconv.Atoi(line[1:])
					args := make([]string, n)
					for i := range args {
						l, err := readLine(r)
						if err != nil {
							return
						}
						size, _ := strconv.Atoi(l[1:])
						buf := make([]byte, size+2)
						if _, err := ioReadFull(r, buf); err != nil {
							return
						}
						args[i] = string(buf[:size])
					}
					mu.Lock()
					switch strings.ToUpper(args[0]) {
					case "PING":
						c.Write([]byte("+PONG\r\n"))
					case "SET":
						data[args[1]] = args[2]
						c.Write([]byte("+OK\r\n"))
					case "GET":
						if v, ok := data[args[1]]; ok {
							c.Write([]byte("$" + strconv.Itoa(len(v)) + "\r\n" + v + "\r\n"))
						} else {
							c.Write([]byte("$-1\r\n"))
						}
					case "DEL":
						delete(data, args[1])
						c.Write([]byte(":1\r\n"))
					default:
						c.Write([]byte("-ERR unknown\r\n"))
					}
					mu.Unlock()
				}
			}(conn)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func TestRedisStoreRoundTripAndFallback(t *testing.T) {
	addr, stop := fakeRedis(t)
	c, _ := newRespClient("redis://" + addr)
	s := &redisStore{c: c, fallback: newLimitMemStore()}

	if v, err := s.Get("k"); err != nil || v != nil {
		t.Fatalf("missing key: %v %v", v, err)
	}
	if err := s.Set("k", []byte("a\x00b\r\n"), time.Minute); err != nil { // binary safe (the limiter stores msgpack)
		t.Fatal(err)
	}
	if v, _ := s.Get("k"); string(v) != "a\x00b\r\n" {
		t.Fatalf("got %q", v)
	}
	_ = s.Delete("k")
	if v, _ := s.Get("k"); v != nil {
		t.Fatalf("deleted key still there: %q", v)
	}

	// Redis goes away: the limiter must keep working on the in-memory copy and never return an error
	stop()
	_, _ = c.do(nilCtx(), "PING") // drop the dead connection
	if err := s.Set("x", []byte("1"), time.Minute); err != nil {
		t.Fatalf("set during outage must not fail: %v", err)
	}
	if v, err := s.Get("x"); err != nil || string(v) != "1" {
		t.Fatalf("get during outage: %q %v", v, err)
	}
}

func TestMemStoreExpiry(t *testing.T) {
	m := newLimitMemStore()
	_ = m.Set("a", []byte("1"), 20*time.Millisecond)
	if v, _ := m.Get("a"); string(v) != "1" {
		t.Fatal("should be present")
	}
	time.Sleep(40 * time.Millisecond)
	if v, _ := m.Get("a"); v != nil {
		t.Fatal("should have expired")
	}
}

func nilCtx() context.Context {
	c, _ := context.WithTimeout(context.Background(), time.Second)
	return c
}

var ioReadFull = io.ReadFull

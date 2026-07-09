package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sync/atomic"
	"time"
)

// fallbackToken is used when we can't extract a fresh token from the fast.com
// JavaScript bundle. It rarely changes, so this is usually good enough.
const fallbackToken = "YXNkZmFzZGxmbnNkYWZoYXNkZmhrYWxm"

var (
	scriptExpr = regexp.MustCompile(`app-[a-z0-9]+\.js`)
	tokenExpr  = regexp.MustCompile(`token:"(\w+)"`)
)

// token extracts the API token from the fast.com JavaScript bundle. fast.com
// embeds it in a script tag, so we fetch the page, find the script and pull the
// token out of it.
func token() string {
	page, err := get("https://fast.com/")
	if err != nil {
		return fallbackToken
	}

	script, err := get("https://fast.com/" + scriptExpr.FindString(string(page)))
	if err != nil {
		return fallbackToken
	}

	match := tokenExpr.FindSubmatch(script)
	if len(match) < 2 {
		return fallbackToken
	}
	return string(match[1])
}

// targets asks fast.com for count URLs to download from. fast.com is powered by
// Netflix, so these point at the Netflix Open Connect servers nearest to us.
func targets(count int) ([]string, error) {
	url := fmt.Sprintf("https://api.fast.com/netflix/speedtest/v2?https=true&token=%s&urlCount=%d", token(), count)
	body, err := get(url)
	if err != nil {
		return nil, err
	}

	var response struct {
		Targets []struct {
			URL string `json:"url"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	urls := make([]string, len(response.Targets))
	for i, target := range response.Targets {
		urls[i] = target.URL
	}
	return urls, nil
}

// download repeatedly downloads from url until the context is cancelled, adding
// the number of bytes it reads to total as it goes. We run a few of these in
// parallel to saturate the connection.
func download(ctx context.Context, url string, total *atomic.Int64) {
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			time.Sleep(100 * time.Millisecond)
			continue
		}

		io.Copy(counter{total}, resp.Body)
		resp.Body.Close()
	}
}

// counter is an io.Writer that keeps a running total of how many bytes have
// been written to it, without keeping any of them around.
type counter struct {
	total *atomic.Int64
}

func (c counter) Write(p []byte) (int, error) {
	c.total.Add(int64(len(p)))
	return len(p), nil
}

// uploadReader is an io.Reader that generates a continuous stream of zeroes
// and keeps a running total of how many bytes have been read from it.
type uploadReader struct {
	total *atomic.Int64
}

func (u uploadReader) Read(p []byte) (int, error) {
	clear(p)
	u.total.Add(int64(len(p)))
	return len(p), nil
}

// upload repeatedly uploads to url until the context is cancelled, adding
// the number of bytes it sends to total as it goes.
func upload(ctx context.Context, url string, total *atomic.Int64) {
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, uploadReader{total})
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		req.Header.Set("Content-Type", "application/octet-stream")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			time.Sleep(100 * time.Millisecond)
			continue
		}

		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// get performs an HTTP GET request and returns the response body.
func get(url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// measurePing measures the TCP connection setup time (1 RTT) to the given URL.
func measurePing(urlStr string) (time.Duration, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return 0, err
	}
	host := u.Hostname()
	
	// Pre-resolve IP to isolate TCP RTT from DNS
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return 0, fmt.Errorf("lookup failed: %v", err)
	}
	
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	
	address := net.JoinHostPort(ips[0].String(), port)

	start := time.Now()
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return 0, err
	}
	conn.Close()
	return time.Since(start), nil
}

// measureDNS measures DNS resolution time using a unique domain
// to bypass local DNS caches.
func measureDNS() time.Duration {
	start := time.Now()
	domain := fmt.Sprintf("%d.com", time.Now().UnixNano())
	_, err := net.LookupHost(domain)
	if err != nil {
		// Ignore error and just return the duration it took
		return time.Since(start)
	}
	return time.Since(start)
}

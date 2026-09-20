package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

type savedCookie struct {
	Origin string      `json:"origin"`
	Cookie http.Cookie `json:"cookie"`
}
type CookieStore struct {
	jar      *cookiejar.Jar
	records  map[string]savedCookie
	file     string
	mu       sync.Mutex
	dirty    map[string]bool
	baseline map[string]savedCookie
}

func cookieKey(origin *url.URL, c http.Cookie) string {
	domain := strings.TrimPrefix(strings.ToLower(c.Domain), ".")
	if domain == "" {
		domain = origin.Hostname()
	}
	return domain + "\t" + c.Path + "\t" + c.Name
}
func defaultCookiePath(u *url.URL) string {
	if u.Path == "" || !strings.HasPrefix(u.Path, "/") {
		return "/"
	}
	i := strings.LastIndex(u.Path, "/")
	if i == 0 {
		return "/"
	}
	return u.Path[:i]
}
func NewCookieStore(p *Profile) (*CookieStore, error) {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	store := &CookieStore{jar: jar, records: map[string]savedCookie{}, file: filepath.Join(p.Path, "cookies.json"), dirty: map[string]bool{}, baseline: map[string]savedCookie{}}
	data, err := os.ReadFile(store.file)
	if errors.Is(err, os.ErrNotExist) {
		legacy := filepath.Join(p.Path, "cookies.txt")
		if _, e := os.Stat(legacy); e == nil {
			if e = store.Import(legacy); e != nil {
				return nil, e
			}
			if e = store.Save(); e != nil {
				return nil, e
			}
		}
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	var records []savedCookie
	if err = json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("cannot read saved cookies: %w", err)
	}
	now := time.Now()
	for _, r := range records {
		u, e := url.Parse(r.Origin)
		if e != nil || u.Host == "" {
			return nil, errors.New("invalid saved cookie origin")
		}
		if !r.Cookie.Expires.IsZero() && !r.Cookie.Expires.After(now) {
			continue
		}
		// Restore absolute expiry, not the original Max-Age duration.
		r.Cookie.MaxAge = 0
		store.jar.SetCookies(u, []*http.Cookie{&r.Cookie})
		store.records[cookieKey(u, r.Cookie)] = r
		store.baseline[cookieKey(u, r.Cookie)] = r
	}
	return store, nil
}
func (s *CookieStore) Cookies(u *url.URL) []*http.Cookie {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jar.Cookies(u)
}
func (s *CookieStore) SetCookies(u *url.URL, cookies []*http.Cookie) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jar.SetCookies(u, cookies)
	for _, cp := range cookies {
		c := *cp
		if c.Path == "" || !strings.HasPrefix(c.Path, "/") {
			c.Path = defaultCookiePath(u)
		}
		key := cookieKey(u, c)
		s.dirty[key] = true
		if c.MaxAge < 0 || (c.MaxAge == 0 && !c.Expires.IsZero() && !c.Expires.After(time.Now())) {
			delete(s.records, key)
			continue
		}
		if c.MaxAge > 0 {
			c.Expires = time.Now().Add(time.Duration(c.MaxAge) * time.Second)
		}
		c.MaxAge = 0
		origin := *u
		origin.RawQuery = ""
		origin.Fragment = ""
		origin.User = nil
		s.records[key] = savedCookie{origin.String(), c}
	}
}
func (s *CookieStore) Save() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.syncStorage(ctx, true)
}
func (s *CookieStore) Refresh(ctx context.Context) error { return s.syncStorage(ctx, false) }

func (s *CookieStore) syncStorage(ctx context.Context, save bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	release, err := acquire(ctx, s.file+".lock", false)
	if err != nil {
		return err
	}
	defer release()
	merged := map[string]savedCookie{}
	data, err := os.ReadFile(s.file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		var records []savedCookie
		if err = json.Unmarshal(data, &records); err != nil {
			return err
		}
		for _, r := range records {
			u, e := url.Parse(r.Origin)
			if e != nil || u.Host == "" {
				return errors.New("invalid saved cookie origin")
			}
			r.Cookie.MaxAge = 0
			merged[cookieKey(u, r.Cookie)] = r
		}
	}
	if save {
		for key := range s.dirty {
			// A stale response must not overwrite a cookie changed by another process.
			current, currentOK := merged[key]
			previous, previousOK := s.baseline[key]
			if currentOK != previousOK || !reflect.DeepEqual(current, previous) {
				continue
			}
			if r, ok := s.records[key]; ok {
				merged[key] = r
			} else {
				delete(merged, key)
			}
		}
	}
	records := []savedCookie{}
	for key, r := range merged {
		if !r.Cookie.Expires.IsZero() && !r.Cookie.Expires.After(time.Now()) {
			delete(merged, key)
			continue
		}
		records = append(records, r)
	}
	if save {
		data, err = json.Marshal(records)
		if err != nil {
			return err
		}
		if err = atomicWrite(s.file, data); err != nil {
			return err
		}
	}
	s.jar, _ = cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	s.records = merged
	s.baseline = map[string]savedCookie{}
	for key, r := range merged {
		u, _ := url.Parse(r.Origin)
		s.jar.SetCookies(u, []*http.Cookie{&r.Cookie})
		s.baseline[key] = r
	}
	s.dirty = map[string]bool{}
	return nil
}
func (s *CookieStore) Import(file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	records := []savedCookie{}
	for scanner.Scan() {
		line := scanner.Text()
		httpOnly := strings.HasPrefix(line, "#HttpOnly_")
		line = strings.TrimPrefix(line, "#HttpOnly_")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			return errors.New("expected a Netscape cookie file")
		}
		host := strings.TrimPrefix(fields[0], ".")
		if host == "" || strings.ContainsAny(host, "/: ") {
			return errors.New("invalid cookie host")
		}
		c := http.Cookie{Name: fields[5], Value: fields[6], Path: fields[2], Secure: fields[3] == "TRUE", HttpOnly: httpOnly}
		if fields[1] == "TRUE" {
			c.Domain = fields[0]
		}
		if fields[4] != "" && fields[4] != "0" {
			n, e := strconv.ParseInt(fields[4], 10, 64)
			if e != nil {
				return errors.New("invalid cookie expiry")
			}
			c.Expires = time.Unix(n, 0)
		}
		scheme := "http"
		if c.Secure {
			scheme = "https"
		}
		u := url.URL{Scheme: scheme, Host: host, Path: path.Clean(c.Path)}
		records = append(records, savedCookie{u.String(), c})
	}
	if err = scanner.Err(); err != nil {
		return err
	}
	for _, r := range records {
		u, _ := url.Parse(r.Origin)
		s.SetCookies(u, []*http.Cookie{&r.Cookie})
	}
	return nil
}

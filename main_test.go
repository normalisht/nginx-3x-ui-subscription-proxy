package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var testLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// source — фейковый источник подписки: отдаёт body, запоминает путь и заголовки запроса.
type source struct {
	*httptest.Server
	path   string
	header http.Header
}

func newSource(t *testing.T, status int, body string) *source {
	t.Helper()
	s := &source{}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.path, s.header = r.URL.Path, r.Header.Clone()
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(s.Close)
	return s
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(rec.Body.String())
	if err != nil {
		t.Fatalf("ответ не base64: %q", rec.Body.String())
	}
	return string(b)
}

func TestDecode(t *testing.T) {
	for name, c := range map[string]struct{ in, want string }{
		"обычный base64":             {b64("vless://a\nvless://b\n"), "vless://a\nvless://b\n"},
		"base64 без паддинга":        {strings.TrimRight(b64("vless://ab"), "="), "vless://ab"},
		"URL-safe base64":            {base64.URLEncoding.EncodeToString([]byte("??>>")), "??>>"},
		"пробелы и перевод строки":   {"\n " + b64("vless://a") + "\n", "vless://a"},
		"обычный текст":              {"vless://a@host:443#Имя\n", "vless://a@host:443#Имя"},
		"похоже на base64, не UTF-8": {base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe, 0xfd}), base64.StdEncoding.EncodeToString([]byte{0xff, 0xfe, 0xfd})},
		"пусто":                      {"  \n", ""},
	} {
		if got := decode(c.in); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

func TestParseHeaders(t *testing.T) {
	got := parseHeaders("User-Agent: Happ/2.4.1 (Android 13);;Hwid: 1234\nбез двоеточия\r\nX-Url: https://a:b ;; ")
	want := http.Header{"User-Agent": {"Happ/2.4.1 (Android 13)"}, "Hwid": {"1234"}, "X-Url": {"https://a:b"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseHeaders = %v, want %v", got, want)
	}
}

func TestSubscriptionID(t *testing.T) {
	for path, want := range map[string]string{
		"/sub/abc":     "abc",
		"/sub/abc/":    "abc",
		"/sub/":        "",
		"/sub/abc/def": "",
		"/other/abc":   "",
		"/subabc":      "",
	} {
		if got, _ := subscriptionID("sub", path); got != want {
			t.Errorf("%s: %q, want %q", path, got, want)
		}
	}
	if got, _ := subscriptionID("api/v1", "/api/v1/abc"); got != "abc" {
		t.Errorf("составной SUB: %q", got)
	}
}

func TestDeepMerge(t *testing.T) {
	a := map[string]any{"list": []any{1}, "obj": map[string]any{"x": 1, "y": 1}, "s": "a"}
	b := map[string]any{"list": []any{2}, "obj": map[string]any{"y": 2, "z": 2}, "s": "b", "new": true}
	want := map[string]any{"list": []any{1, 2}, "obj": map[string]any{"x": 1, "y": 2, "z": 2}, "s": "b", "new": true}
	if got := deepMerge(a, b); !reflect.DeepEqual(got, want) {
		t.Errorf("deepMerge = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(a["list"], []any{1}) {
		t.Errorf("deepMerge изменил исходный список: %v", a["list"])
	}
	if got := deepMerge([]any{1}, map[string]any{"k": 1}); !reflect.DeepEqual(got, map[string]any{"k": 1}) {
		t.Errorf("разные типы: %v", got)
	}
}

func TestBase64Subscription(t *testing.T) {
	s1 := newSource(t, http.StatusOK, b64("vless://one\n"))
	s2 := newSource(t, http.StatusOK, "vless://two") // обычный текст, без base64
	down := newSource(t, http.StatusInternalServerError, "ошибка")
	ext := newSource(t, http.StatusOK, b64("vless://ext\n"))

	c := config{
		SiteHost: "sub.example", Sub: "sub", Routing: true,
		Servers:         []string{s1.URL + "/sub/", down.URL + "/sub", s2.URL + "/x"},
		External:        []string{ext.URL + "/ready/link"},
		ExternalHeaders: http.Header{"Hwid": {"42"}},
	}
	rec := get(t, newHandler(c, newFetcher(testLog), testLog), "/sub/client1")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("ответ: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	want := "://autorouting/onadd/https://sub.example/routing/routing.json\n" +
		"://routing/onadd/https://sub.example/routing/routing.json\n" +
		"vless://one\nvless://two\nvless://ext\n"
	if got := decodeBody(t, rec); got != want {
		t.Errorf("подписка:\n%s\nwant:\n%s", got, want)
	}
	if s1.path != "/sub/client1" || s2.path != "/x/client1" || ext.path != "/ready/link" {
		t.Errorf("пути: %q %q %q", s1.path, s2.path, ext.path)
	}
	if ext.header.Get("Hwid") != "42" || s1.header.Get("Hwid") != "" {
		t.Errorf("заголовки только сторонним подпискам: ext %q, 3x-ui %q", ext.header.Get("Hwid"), s1.header.Get("Hwid"))
	}
}

func TestJSONSubscription(t *testing.T) {
	a := newSource(t, http.StatusOK, b64(`[{"remarks":"Германия","port":443},{"remarks":"Авто"}]`))
	b := newSource(t, http.StatusOK, `[{"remarks":"Германия","big":12345678901234567890,"html":"<a&b>"}]`)
	bad := newSource(t, http.StatusOK, "не json")
	srv3x := newSource(t, http.StatusOK, b64("vless://3x-ui\n"))

	c := config{Sub: "sub", JSON: true, Routing: true, SiteHost: "x",
		Servers: []string{srv3x.URL}, External: []string{a.URL, bad.URL, b.URL}}
	rec := get(t, newHandler(c, newFetcher(testLog), testLog), "/sub/id")
	if rec.Code != http.StatusOK {
		t.Fatalf("ответ: %d %s", rec.Code, rec.Body)
	}
	got := decodeBody(t, rec)
	var profiles []map[string]any
	if err := json.Unmarshal([]byte(got), &profiles); err != nil {
		t.Fatalf("не JSON: %s", got)
	}
	var names []string
	for _, p := range profiles {
		names = append(names, p["remarks"].(string))
	}
	if !reflect.DeepEqual(names, []string{"Германия", "Авто", "Германия (2)"}) {
		t.Errorf("профили: %v", names)
	}
	// Большие числа и спецсимволы — как в источнике; 3x-UI и строки routing в JSON-режиме не участвуют.
	if !strings.Contains(got, `12345678901234567890`) || !strings.Contains(got, `"<a&b>"`) || srv3x.path != "" {
		t.Errorf("JSON: %s; запрос к 3x-UI: %q", got, srv3x.path)
	}
}

func TestNoConfigs(t *testing.T) {
	down := newSource(t, http.StatusBadGateway, "")
	empty := newSource(t, http.StatusOK, "\n")
	for name, c := range map[string]config{
		"источники недоступны": {Sub: "sub", Servers: []string{down.URL}, External: []string{empty.URL}},
		"JSON без ответов":     {Sub: "sub", JSON: true, External: []string{down.URL}},
	} {
		rec := get(t, newHandler(c, newFetcher(testLog), testLog), "/sub/id")
		if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "No configs available") {
			t.Errorf("%s: %d %q", name, rec.Code, rec.Body)
		}
	}
}

func TestRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "routing.json"), []byte(`{"Name":"Personal"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	src := newSource(t, http.StatusOK, "vless://a")
	h := newHandler(config{Sub: "sub", Servers: []string{src.URL}, RoutingDir: dir}, newFetcher(testLog), testLog)

	for path, want := range map[string]int{
		"/healthz":              http.StatusOK,
		"/routing/routing.json": http.StatusOK,
		"/routing/":             http.StatusNotFound,
		"/routing/nope.json":    http.StatusNotFound,
		"/":                     http.StatusNotFound,
		"/other/id":             http.StatusNotFound,
		"/sub/id":               http.StatusOK,
	} {
		if rec := get(t, h, path); rec.Code != want {
			t.Errorf("%s: %d, want %d", path, rec.Code, want)
		}
	}
	if rec := get(t, h, "/routing/routing.json"); rec.Body.String() != `{"Name":"Personal"}` {
		t.Errorf("routing.json: %q", rec.Body)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sub/id", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", rec.Code)
	}
}

func TestLoadConfig(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	c, err := loadConfig(env(map[string]string{"SERVERS": " https://a/sub/  https://b/sub/ ", "SUB": "/sub/"}))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Servers, []string{"https://a/sub/", "https://b/sub/"}) || c.Sub != "sub" ||
		c.HTTPAddr != ":8080" || c.HTTPSAddr != ":8443" || c.TLS {
		t.Errorf("config = %+v", c)
	}
	if c, _ := loadConfig(env(map[string]string{"EXTERNAL_SUBSCRIPTIONS": "https://x"})); c.Sub != "sub" {
		t.Errorf("SUB по умолчанию: %q", c.Sub)
	}

	for name, m := range map[string]map[string]string{
		"нет источников":        {"SUB": "sub"},
		"TLS без SITE_HOST":     {"SERVERS": "https://a", "TLS": "1"},
		"routing без SITE_HOST": {"SERVERS": "https://a", "ROUTING_ENABLED": "1"},
	} {
		if _, err := loadConfig(env(m)); err == nil {
			t.Errorf("%s: want ошибку", name)
		}
	}
}

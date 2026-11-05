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
	"time"
)

var testLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// source — фейковый источник подписки: отдаёт body, запоминает путь и заголовки запроса, считает запросы.
// Ответ можно поменять между запросами; abort обрывает соединение посреди тела.
type source struct {
	*httptest.Server
	path   string
	header http.Header
	hits   int
	status int
	body   string
	abort  bool
}

func newSource(t *testing.T, status int, body string) *source {
	t.Helper()
	s := &source{status: status, body: body}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.path, s.header = r.URL.Path, r.Header.Clone()
		s.hits++
		w.WriteHeader(s.status)
		io.WriteString(w, s.body)
		if s.abort {
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// clockedFetcher — fetcher с управляемыми часами: advance переводит их вперёд.
func clockedFetcher(log *slog.Logger) (f *fetcher, advance func(time.Duration)) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	f = newFetcher(log)
	f.now = func() time.Time { return now }
	return f, func(d time.Duration) { now = now.Add(d) }
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
		"/sub/a-b_C9":  "a-b_C9",
		"/sub/abc?x=1": "",
		"/sub/abc#x":   "",
		"/sub/..":      "",
		"/sub/a.b":     "",
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
	// Happ не разбирает JSON в base64: ответ — чистый JSON, как у самого источника.
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type: %q", ct)
	}
	got := rec.Body.String()
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

// В лог попадает только хост источника: id подписки в пути — секрет.
func TestLogWithoutSubscriptionID(t *testing.T) {
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, nil))
	down := newSource(t, http.StatusOK, "")
	down.Close() // соединение будет отклонено
	notFound := newSource(t, http.StatusNotFound, "")
	c := config{Sub: "sub", Servers: []string{down.URL + "/secret-path/", notFound.URL + "/secret-path/"}}
	get(t, newHandler(c, newFetcher(log), log), "/sub/secretid")
	if !strings.Contains(buf.String(), "источник недоступен") || !strings.Contains(buf.String(), "источник ответил ошибкой") {
		t.Fatalf("нет записей об ошибках: %s", buf.String())
	}
	if strings.Contains(buf.String(), "secretid") || strings.Contains(buf.String(), "secret-path") {
		t.Errorf("в логе адрес подписки: %s", buf.String())
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

func TestFreshResponseSkipsSource(t *testing.T) {
	s := newSource(t, http.StatusOK, "vless://old")
	f, advance := clockedFetcher(testLog)
	h := newHandler(config{Sub: "sub", External: []string{s.URL}}, f, testLog)

	if got := decodeBody(t, get(t, h, "/sub/id")); got != "vless://old\n" {
		t.Fatalf("первый ответ: %q", got)
	}
	s.body = "vless://new"
	advance(5*time.Minute - time.Second)
	if got := decodeBody(t, get(t, h, "/sub/id")); got != "vless://old\n" || s.hits != 1 {
		t.Errorf("пока ответ свежий: %q, запросов к источнику %d", got, s.hits)
	}
	advance(time.Second)
	if got := decodeBody(t, get(t, h, "/sub/id")); got != "vless://new\n" || s.hits != 2 {
		t.Errorf("через 5 минут: %q, запросов к источнику %d", got, s.hits)
	}
}

// Адреса серверов 3x-UI содержат id: ответ одного клиента нельзя отдать другому.
func TestResponseNotSharedBetweenClients(t *testing.T) {
	s := newSource(t, http.StatusOK, "vless://a")
	h := newHandler(config{Sub: "sub", Servers: []string{s.URL + "/sub"}}, newFetcher(testLog), testLog)
	if rec := get(t, h, "/sub/client-a"); rec.Code != http.StatusOK {
		t.Fatalf("client-a: %d", rec.Code)
	}
	s.status = http.StatusInternalServerError
	if rec := get(t, h, "/sub/client-b"); rec.Code != http.StatusBadGateway || s.path != "/sub/client-b" {
		t.Errorf("client-b: %d, путь у источника %q", rec.Code, s.path)
	}
}

func TestSourceAnswerAfterFreshPeriod(t *testing.T) {
	const plain, profiles = "vless://a", `[{"remarks":"A"}]`
	for name, c := range map[string]struct {
		json     bool
		status   int
		body     string
		abort    bool
		closed   bool
		wantCode int
		want     string
	}{
		"соединение отклонено → прошлый ответ": {closed: true, wantCode: 200, want: "vless://a\n"},
		"обрыв тела → прошлый ответ":           {status: 200, body: "vless", abort: true, wantCode: 200, want: "vless://a\n"},
		"500 → прошлый ответ":                  {status: 500, wantCode: 200, want: "vless://a\n"},
		"408 → прошлый ответ":                  {status: 408, wantCode: 200, want: "vless://a\n"},
		"429 → прошлый ответ":                  {status: 429, wantCode: 200, want: "vless://a\n"},
		"404: подписки больше нет":             {status: 404, wantCode: 502},
		"403: подписка отозвана":               {status: 403, wantCode: 502},
		"200 и пусто: подписка без серверов":   {status: 200, body: "\n", wantCode: 502},
		"200 и новый текст":                    {status: 200, body: "vless://b", wantCode: 200, want: "vless://b\n"},
		"JSON: 200 и не JSON → прошлый ответ":  {json: true, status: 200, body: "vless://stub", wantCode: 200, want: profiles},
		"JSON: 500 → прошлый ответ":            {json: true, status: 500, wantCode: 200, want: profiles},
		"JSON: 200 и новый JSON":               {json: true, status: 200, body: `[{"remarks":"B"}]`, wantCode: 200, want: `[{"remarks":"B"}]`},
	} {
		t.Run(name, func(t *testing.T) {
			first := plain
			if c.json {
				first = profiles
			}
			s := newSource(t, http.StatusOK, first)
			f, advance := clockedFetcher(testLog)
			h := newHandler(config{Sub: "sub", JSON: c.json, External: []string{s.URL}}, f, testLog)
			if rec := get(t, h, "/sub/id"); rec.Code != http.StatusOK {
				t.Fatalf("первый запрос: %d", rec.Code)
			}

			advance(5 * time.Minute)
			s.status, s.body, s.abort = c.status, c.body, c.abort
			if c.closed {
				s.Close()
			}
			rec := get(t, h, "/sub/id")
			if rec.Code != c.wantCode {
				t.Fatalf("код %d, want %d", rec.Code, c.wantCode)
			}
			if c.wantCode == http.StatusOK {
				got := rec.Body.String()
				if !c.json {
					got = decodeBody(t, rec)
				}
				if got != c.want {
					t.Errorf("подписка %q, want %q", got, c.want)
				}
			}
		})
	}
}

// Ответ, отданный из-за сбоя, не становится свежим: источник опрашивается на следующем же запросе.
func TestFallbackKeepsPolling(t *testing.T) {
	s := newSource(t, http.StatusOK, "vless://old")
	f, advance := clockedFetcher(testLog)
	h := newHandler(config{Sub: "sub", External: []string{s.URL}}, f, testLog)
	get(t, h, "/sub/id")

	advance(5 * time.Minute)
	s.status = http.StatusInternalServerError
	if got := decodeBody(t, get(t, h, "/sub/id")); got != "vless://old\n" || s.hits != 2 {
		t.Fatalf("при сбое: %q, запросов %d", got, s.hits)
	}
	s.status, s.body = http.StatusOK, "vless://new"
	if got := decodeBody(t, get(t, h, "/sub/id")); got != "vless://new\n" || s.hits != 3 {
		t.Errorf("после восстановления: %q, запросов %d", got, s.hits)
	}
}

// Отказ источника отменяет прошлый ответ: при последующем сбое он уже не вернётся клиенту.
func TestRefusalForgetsResponse(t *testing.T) {
	for name, refusal := range map[string]struct {
		status int
		body   string
	}{
		"404":         {status: http.StatusNotFound},
		"200 и пусто": {status: http.StatusOK, body: "\n"},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSource(t, http.StatusOK, "vless://a")
			f, advance := clockedFetcher(testLog)
			h := newHandler(config{Sub: "sub", External: []string{s.URL}}, f, testLog)
			get(t, h, "/sub/id")

			advance(5 * time.Minute)
			s.status, s.body = refusal.status, refusal.body
			get(t, h, "/sub/id")
			s.status, s.body = http.StatusInternalServerError, ""
			if rec := get(t, h, "/sub/id"); rec.Code != http.StatusBadGateway {
				t.Errorf("после отказа и сбоя: %d %q", rec.Code, rec.Body)
			}
		})
	}
}

// Сбой одного источника не урезает подписку до профилей остальных, а номера в remarks не накапливаются.
func TestJSONKeepsProfilesOfFailedSource(t *testing.T) {
	const want = `[{"remarks":"A"},{"remarks":"A (2)"}]`
	a := newSource(t, http.StatusOK, `[{"remarks":"A"}]`)
	b := newSource(t, http.StatusOK, `[{"remarks":"A"}]`)
	f, advance := clockedFetcher(testLog)
	h := newHandler(config{Sub: "sub", JSON: true, External: []string{a.URL, b.URL}}, f, testLog)
	if got := get(t, h, "/sub/id").Body.String(); got != want {
		t.Fatalf("оба источника: %s", got)
	}
	advance(5 * time.Minute)
	b.status = http.StatusBadGateway
	if got := get(t, h, "/sub/id").Body.String(); got != want {
		t.Errorf("сбой второго источника: %s, want %s", got, want)
	}
}

func TestFallbackLogHasHostOnly(t *testing.T) {
	var buf strings.Builder
	log := slog.New(slog.NewTextHandler(&buf, nil))
	s := newSource(t, http.StatusOK, "vless://a")
	f, advance := clockedFetcher(log)
	h := newHandler(config{Sub: "sub", Servers: []string{s.URL + "/secret-path/"}}, f, log)
	get(t, h, "/sub/secretid")

	advance(5 * time.Minute)
	s.status = http.StatusInternalServerError
	if rec := get(t, h, "/sub/secretid"); rec.Code != http.StatusOK {
		t.Fatalf("код %d, want прошлый ответ", rec.Code)
	}
	if !strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("нет предупреждения о прошлом ответе: %s", buf.String())
	}
	if strings.Contains(buf.String(), "secretid") || strings.Contains(buf.String(), "secret-path") {
		t.Errorf("в логе адрес подписки: %s", buf.String())
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

func TestAllowedIDs(t *testing.T) {
	for name, c := range map[string]struct {
		json    bool
		allowed []string
		path    string
		want    int
		wantHit int
	}{
		"в списке":                     {allowed: []string{"good", "other"}, path: "/sub/good", want: 200, wantHit: 1},
		"второй в списке":              {allowed: []string{"good", "other"}, path: "/sub/other", want: 200, wantHit: 1},
		"в списке, «/» на конце":       {allowed: []string{"good"}, path: "/sub/good/", want: 200, wantHit: 1},
		"не в списке":                  {allowed: []string{"good"}, path: "/sub/testid", want: 404},
		"начало разрешённого id":       {allowed: []string{"good"}, path: "/sub/goo", want: 404},
		"другой регистр":               {allowed: []string{"good"}, path: "/sub/GOOD", want: 404},
		"список пуст — любой id":       {path: "/sub/testid", want: 200, wantHit: 1},
		"JSON: id не в списке":         {json: true, allowed: []string{"good"}, path: "/sub/testid", want: 404},
		"JSON: id в списке":            {json: true, allowed: []string{"good"}, path: "/sub/good", want: 200, wantHit: 1},
		"JSON: список пуст — любой id": {json: true, path: "/sub/testid", want: 200, wantHit: 1},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSource(t, http.StatusOK, `[{"remarks":"A"}]`)
			cfg := config{Sub: "sub", JSON: c.json, AllowedIDs: c.allowed, External: []string{s.URL}}
			h := newHandler(cfg, newFetcher(testLog), testLog)
			if rec := get(t, h, c.path); rec.Code != c.want {
				t.Errorf("код %d, want %d", rec.Code, c.want)
			}
			if s.hits != c.wantHit {
				t.Errorf("запросов к источнику %d, want %d", s.hits, c.wantHit)
			}
		})
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
	c, _ = loadConfig(env(map[string]string{"SERVERS": "https://a", "ALLOWED_IDS": " one  two\n"}))
	if !reflect.DeepEqual(c.AllowedIDs, []string{"one", "two"}) {
		t.Errorf("ALLOWED_IDS: %q", c.AllowedIDs)
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

package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// maxBody — предел ответа одного источника: подписка — килобайты, JSON-конфиги — сотни килобайт.
const maxBody = 10 << 20

type fetcher struct {
	client *http.Client
	log    *slog.Logger
}

func newFetcher(log *slog.Logger) *fetcher {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	// Проверка сертификата выключена намеренно: у серверов 3x-UI бывают самоподписанные.
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return &fetcher{client: &http.Client{Timeout: 10 * time.Second, Transport: tr}, log: log}
}

// get скачивает источник и декодирует base64, если ответ на него похож. Пустая строка — источник
// недоступен или пуст: его просто пропускают.
func (f *fetcher) get(ctx context.Context, rawURL string, headers http.Header) string {
	// В логе — только хост: в пути и параметрах ссылки — id подписки.
	host := rawURL
	if u, err := url.Parse(rawURL); err == nil {
		host = u.Host
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		f.log.Error("неверный адрес источника", "host", host, "err", err)
		return ""
	}
	for k, v := range headers {
		req.Header[k] = v
	}
	resp, err := f.client.Do(req)
	if err != nil {
		// url.Error повторяет в тексте полный адрес — оставляем только причину.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		f.log.Error("источник недоступен", "host", host, "err", err)
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		f.log.Error("источник ответил ошибкой", "host", host, "status", resp.StatusCode)
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		f.log.Error("ответ источника не прочитался", "host", host, "err", err)
		return ""
	}
	return decode(string(body))
}

var base64Re = regexp.MustCompile(`^[A-Za-z0-9+/_=-]+$`)

// decode возвращает раскодированный base64 (обычный или URL-safe, с паддингом или без), если текст
// им является и внутри UTF-8; иначе — текст как есть, без пробелов по краям.
func decode(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || !base64Re.MatchString(text) {
		return text
	}
	padded := strings.TrimRight(text, "=")
	padded += strings.Repeat("=", (4-len(padded)%4)%4)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding} {
		if b, err := enc.DecodeString(padded); err == nil && utf8.Valid(b) {
			return string(b)
		}
	}
	return text
}

// fetchAll скачивает источники параллельно; результаты — в порядке urls.
func (f *fetcher) fetchAll(ctx context.Context, urls []string, headers http.Header) []string {
	out := make([]string, len(urls))
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Go(func() { out[i] = f.get(ctx, u, headers) })
	}
	wg.Wait()
	return out
}

// subscription собирает подписку клиента id в base64. ok=false — ни один источник ничего не дал.
func (f *fetcher) subscription(ctx context.Context, c config, id string) (body string, ok bool) {
	if c.JSON {
		return f.jsonSubscription(ctx, c)
	}

	var b strings.Builder
	if c.Routing {
		// Клиенты вроде Happ по этим строкам подтягивают правила маршрутизации.
		fmt.Fprintf(&b, "://autorouting/onadd/https://%s/routing/routing.json\n", c.SiteHost)
		fmt.Fprintf(&b, "://routing/onadd/https://%s/routing/routing.json\n", c.SiteHost)
	}
	urls := make([]string, 0, len(c.Servers)+len(c.External))
	for _, s := range c.Servers {
		urls = append(urls, strings.TrimRight(s, "/")+"/"+id)
	}
	results := f.fetchAll(ctx, urls, nil)
	results = append(results, f.fetchAll(ctx, c.External, c.ExternalHeaders)...)
	for _, r := range results {
		if r != "" {
			b.WriteString(strings.TrimRight(r, "\n") + "\n")
		}
	}
	if b.Len() == 0 {
		return "", false
	}
	return base64.StdEncoding.EncodeToString([]byte(b.String())), true
}

// jsonSubscription сливает JSON-конфиги сторонних подписок в один (см. deepMerge).
func (f *fetcher) jsonSubscription(ctx context.Context, c config) (string, bool) {
	var merged any
	n := 0
	for i, raw := range f.fetchAll(ctx, c.External, c.ExternalHeaders) {
		if raw == "" {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.UseNumber() // числа — как в источнике, без округления до float64
		var v any
		if err := dec.Decode(&v); err != nil {
			f.log.Error("источник отдал не JSON", "index", i, "err", err)
			continue
		}
		if n == 0 {
			merged = v
		} else {
			merged = deepMerge(merged, v)
		}
		n++
	}
	if n == 0 {
		return "", false
	}
	if list, ok := merged.([]any); ok {
		dedupeRemarks(list)
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(merged); err != nil {
		f.log.Error("сборка JSON", "err", err)
		return "", false
	}
	return base64.StdEncoding.EncodeToString([]byte(strings.TrimRight(b.String(), "\n"))), true
}

// deepMerge: списки склеиваются, объекты сливаются рекурсивно, в остальных случаях побеждает b.
func deepMerge(a, b any) any {
	switch av := a.(type) {
	case []any:
		if bv, ok := b.([]any); ok {
			return append(append([]any{}, av...), bv...)
		}
	case map[string]any:
		if bv, ok := b.(map[string]any); ok {
			out := make(map[string]any, len(av)+len(bv))
			for k, v := range av {
				out[k] = v
			}
			for k, v := range bv {
				if old, ok := out[k]; ok {
					out[k] = deepMerge(old, v)
				} else {
					out[k] = v
				}
			}
			return out
		}
	}
	return b
}

// dedupeRemarks дописывает номер к повторяющимся названиям профилей: «Германия», «Германия (2)».
// Иначе клиент показывает одинаковые строки, и не понять, какая из какой подписки.
func dedupeRemarks(profiles []any) {
	seen := map[string]int{}
	for _, p := range profiles {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		name, ok := m["remarks"].(string)
		if !ok {
			continue
		}
		seen[name]++
		if seen[name] > 1 {
			m["remarks"] = fmt.Sprintf("%s (%d)", name, seen[name])
		}
	}
}

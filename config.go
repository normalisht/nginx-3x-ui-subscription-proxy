package main

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
)

type config struct {
	// SiteHost — домен сервиса: для сертификата и ссылки на routing.json в подписке.
	SiteHost string
	// Sub — статическая часть пути подписки: /<Sub>/<id>.
	Sub string
	// Servers — адреса подписок 3x-UI, к ним дописывается id клиента.
	Servers []string
	// External — готовые ссылки сторонних подписок, запрашиваются как есть.
	External []string
	// ExternalHeaders — заголовки, с которыми запрашиваются External (имитация обычного VPN-клиента).
	ExternalHeaders http.Header
	// Routing — добавлять в подписку строки со ссылкой на routing.json (ROUTING_ENABLED=1).
	Routing bool
	// JSON — сторонние подписки отдают JSON-конфиги: они сливаются в один, Servers не используются.
	JSON bool
	// TLS — сервис сам держит HTTPS с сертификатом Let's Encrypt; иначе — HTTP за общим reverse-proxy.
	TLS     bool
	Email   string
	CertDir string
	// RoutingDir — откуда отдавать /routing/.
	RoutingDir string
	// HTTPAddr и HTTPSAddr — порты внутри контейнера, непривилегированные: процесс работает не от root.
	// Снаружи их публикует compose (80 и 443) или к ним ходит общий reverse-proxy.
	HTTPAddr  string
	HTTPSAddr string
}

func loadConfig(getenv func(string) string) (config, error) {
	c := config{
		SiteHost:        strings.TrimSpace(getenv("SITE_HOST")),
		Sub:             strings.Trim(strings.TrimSpace(getenv("SUB")), "/"),
		Servers:         strings.Fields(getenv("SERVERS")),
		External:        strings.Fields(getenv("EXTERNAL_SUBSCRIPTIONS")),
		ExternalHeaders: parseHeaders(getenv("EXTERNAL_SUBSCRIPTION_HEADERS")),
		Routing:         getenv("ROUTING_ENABLED") == "1",
		JSON:            getenv("EXTERNAL_SUBSCRIPTIONS_JSON") == "1",
		TLS:             getenv("TLS") == "1",
		Email:           strings.TrimSpace(getenv("EMAIL")),
		CertDir:         or(getenv("CERT_DIR"), "/data/certs"),
		RoutingDir:      or(getenv("ROUTING_DIR"), "/srv/routing"),
		HTTPAddr:        or(getenv("HTTP_ADDR"), ":8080"),
		HTTPSAddr:       or(getenv("HTTPS_ADDR"), ":8443"),
	}
	if c.Sub == "" {
		c.Sub = "sub"
	}
	var errs []error
	if c.TLS && c.SiteHost == "" {
		errs = append(errs, errors.New("для TLS=1 нужен SITE_HOST — на него выпускается сертификат"))
	}
	if c.Routing && c.SiteHost == "" {
		errs = append(errs, errors.New("для ROUTING_ENABLED=1 нужен SITE_HOST — он входит в ссылку на routing.json"))
	}
	if len(c.Servers) == 0 && len(c.External) == 0 {
		errs = append(errs, errors.New("не заданы ни SERVERS, ни EXTERNAL_SUBSCRIPTIONS"))
	}
	return c, errors.Join(errs...)
}

// parseHeaders разбирает «Key: Value», разделённые «;;» или переносом строки. Строки без «:» пропускаются.
func parseHeaders(raw string) http.Header {
	h := http.Header{}
	for _, entry := range regexp.MustCompile(`\r?\n|;;`).Split(raw, -1) {
		key, value, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		h.Set(strings.TrimSpace(key), strings.TrimSpace(value))
	}
	return h
}

func or(v, def string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return def
}

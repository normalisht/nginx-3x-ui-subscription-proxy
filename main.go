// subproxy объединяет подписки нескольких серверов 3x-UI и сторонних VPN-сервисов в одну ссылку.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	var err error
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		err = healthcheck()
	} else {
		var c config
		if c, err = loadConfig(os.Getenv); err == nil {
			err = run(c, log)
		}
	}
	if err != nil {
		log.Error("остановлен с ошибкой", "err", err)
		os.Exit(1)
	}
}

func run(c config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	h := newHandler(c, newFetcher(log), log)
	var servers []*http.Server
	if c.TLS {
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(c.SiteHost),
			Cache:      autocert.DirCache(c.CertDir),
			Email:      c.Email,
		}
		// На 80 — проверка Let's Encrypt (http-01), /healthz и редирект на HTTPS.
		plain := http.NewServeMux()
		plain.HandleFunc("GET /healthz", healthz)
		plain.Handle("/", m.HTTPHandler(nil))
		servers = append(servers,
			&http.Server{Addr: c.HTTPAddr, Handler: plain, ReadHeaderTimeout: 10 * time.Second},
			&http.Server{Addr: c.HTTPSAddr, Handler: h, TLSConfig: m.TLSConfig(), ReadHeaderTimeout: 10 * time.Second},
		)
	} else {
		servers = append(servers, &http.Server{Addr: c.HTTPAddr, Handler: h, ReadHeaderTimeout: 10 * time.Second})
	}

	errc := make(chan error, len(servers))
	for _, srv := range servers {
		go func() {
			log.Info("HTTP запущен", "addr", srv.Addr, "tls", srv.TLSConfig != nil)
			var err error
			if srv.TLSConfig != nil {
				err = srv.ListenAndServeTLS("", "")
			} else {
				err = srv.ListenAndServe()
			}
			if !errors.Is(err, http.ErrServerClosed) {
				errc <- err
			}
		}()
	}

	var err error
	select {
	case <-ctx.Done():
	case err = <-errc:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range servers {
		srv.Shutdown(shutdownCtx)
	}
	log.Info("остановлен")
	return err
}

func newHandler(c config, f *fetcher, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	routing := http.StripPrefix("/routing/", http.FileServer(http.Dir(c.RoutingDir)))
	mux.HandleFunc("GET /routing/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") { // без списка файлов каталога
			http.NotFound(w, r)
			return
		}
		routing.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		id, ok := subscriptionID(c.Sub, r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		body, ok := f.subscription(r.Context(), c, id)
		if !ok {
			log.Warn("подписка пуста: ни один источник не ответил")
			http.Error(w, "No configs available", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(body))
	})
	return mux
}

// subscriptionID достаёт id из пути /<sub>/<id> (допустим «/» на конце).
func subscriptionID(sub, path string) (string, bool) {
	id, ok := strings.CutPrefix(path, "/"+sub+"/")
	id = strings.TrimSuffix(id, "/")
	if !ok || id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

func healthz(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) }

// healthcheck — для Docker: в distroless нет ни curl, ни wget.
func healthcheck() error {
	_, port, err := net.SplitHostPort(or(os.Getenv("HTTP_ADDR"), ":8080"))
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz: %s", resp.Status)
	}
	return nil
}

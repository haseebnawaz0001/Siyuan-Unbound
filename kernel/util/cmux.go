package util

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"

	"github.com/siyuan-note/logging"
	"github.com/soheilhy/cmux"
)

// ServeMultiplexed serves both HTTP and HTTPS (including HTTP/2) on the same listener.
//
// httpServer / httpsServer are the *http.Server instances that carry the two kinds of connections; when nil is
// passed, they are created internally.
// It returns the two servers actually used, so the caller can close their active connections when needed (e.g.
// the publish service).
//
// Note: the listener derived by cmux internally embeds the underlying root listener, so calling Close on it
// actually closes root. Therefore HTTP and HTTPS must each use their own *http.Server and must not share one --
// otherwise closing the shared server would also close root, causing m.Serve to return prematurely with a
// non-closed-type error.
func ServeMultiplexed(ln net.Listener, handler http.Handler, certPath, keyPath string, httpServer, httpsServer *http.Server) (*http.Server, *http.Server, error) {
	m := cmux.New(ln)

	tlsL := m.Match(cmux.TLS())
	httpL := m.Match(cmux.Any())

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		logging.LogErrorf("failed to load TLS cert for multiplexing: %s", err)
		return nil, nil, err
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h2", "http/1.1"},
	}

	tlsListener := tls.NewListener(tlsL, tlsConfig)

	if httpServer == nil {
		httpServer = &http.Server{Handler: handler}
	} else {
		httpServer.Handler = handler
	}
	if httpsServer == nil {
		httpsServer = &http.Server{Handler: handler}
	} else {
		httpsServer.Handler = handler
	}

	go func() {
		if serveErr := httpServer.Serve(httpL); serveErr != nil && !errors.Is(serveErr, cmux.ErrListenerClosed) && !errors.Is(serveErr, http.ErrServerClosed) {
			logging.LogErrorf("multiplexed HTTP server error: %s", serveErr)
		}
	}()

	go func() {
		if serveErr := httpsServer.Serve(tlsListener); serveErr != nil && !errors.Is(serveErr, cmux.ErrListenerClosed) && !errors.Is(serveErr, http.ErrServerClosed) {
			logging.LogErrorf("multiplexed HTTPS server error: %s", serveErr)
		}
	}()

	return httpServer, httpsServer, m.Serve()
}

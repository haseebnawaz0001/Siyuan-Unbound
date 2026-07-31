// SiYuan - Refactor your thinking
// Copyright (c) 2020-present, b3log.org
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package util

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/soheilhy/cmux"
)

// writeSelfSignedCert generates a self-signed TLS certificate, writes it to t.TempDir(), and returns the cert and
// private key paths.
// Tests can't rely on the workspace's ConfDir, so this generates its own certificate for ServeMultiplexed's
// tls.LoadX509KeyPair to load.
// The temp directory is automatically cleaned up by the testing framework after the test finishes.
func writeSelfSignedCert(t *testing.T) (certPath, keyPath string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key failed: %s", err)
	}

	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "siyuan-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate failed: %s", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key failed: %s", err)
	}

	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	if err = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write cert failed: %s", err)
	}
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write key failed: %s", err)
	}
	return certPath, keyPath
}

// newTestHandler builds a simple HTTP handler.
func newTestHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	})
	return mux
}

// awaitReady polls until a TCP connection can be established to the target address, or fails on timeout.
func awaitReady(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s did not become ready", addr)
}

// Regression guard: closing a cmux-derived listener closes the underlying root listener.
// This is the root cause of a historical regression -- when HTTP/HTTPS share a server, calling Close on that
// server closes root via both derived listeners, which makes m.Serve return prematurely with a non-closed-type
// error (accept ...: use of closed network connection), triggering kernel exit code 21.
func TestCmuxDerivedListenerCloseClosesRoot(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	m := cmux.New(ln)
	derived := m.Match(cmux.Any())

	serveErrCh := make(chan error, 1)
	go func() { serveErrCh <- m.Serve() }()

	if err := derived.Close(); err != nil {
		t.Logf("derived listener close: %s", err)
	}

	// m.Serve should return because root got closed, and the returned error should NOT be cmux.ErrListenerClosed
	// (root.Accept returns the underlying "use of closed network connection"; if the caller only checks
	// ErrListenerClosed/ErrServerClosed, it would be mistakenly treated as a fatal error).
	select {
	case err := <-serveErrCh:
		if errors.Is(err, cmux.ErrListenerClosed) {
			t.Fatalf("m.Serve should NOT return cmux.ErrListenerClosed, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("m.Serve did not return after closing derived listener")
	}

	// root is already closed, new connections should be refused
	if c, err := net.Dial("tcp", addr); err == nil {
		c.Close()
		t.Fatal("expected root listener to be closed, but connection succeeded")
	}
}

// Regression guard: HTTP and HTTPS must each use their own independent *http.Server.
// If the same server is reused to Serve two derived listeners, calling Close on that server also closes root,
// causing m.Serve to prematurely return "use of closed network connection" -- this is exactly the cause of the
// historical regression (kernel exit code 21, "listen on port failed" popup). This test simulates the publish
// service pre-creating and passing in two independent servers, verifying they are different instances and that
// closing the service returns cleanly.
func TestServeMultiplexed_HTTPAndHTTPSMustUseSeparateServers(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	handler := newTestHandler()
	certPath, keyPath := writeSelfSignedCert(t)

	// Publish service mode: the caller pre-creates and holds two independent servers, passing them in
	pubHTTP := &http.Server{Handler: handler}
	pubHTTPS := &http.Server{Handler: handler}
	if pubHTTP == pubHTTPS {
		t.Fatal("HTTP and HTTPS servers must be independent instances")
	}

	serveErrCh := make(chan error, 1)
	go func() {
		_, _, e := ServeMultiplexed(ln, handler, certPath, keyPath, pubHTTP, pubHTTPS)
		serveErrCh <- e
	}()

	awaitReady(t, addr)

	// Close the HTTP server held by the caller: should let ServeMultiplexed return cleanly
	pubHTTP.Close()

	select {
	case <-serveErrCh:
		// Returning cleanly is a pass (returning "use of closed" is expected, since closing the derived listener closes root)
	case <-time.After(3 * time.Second):
		t.Fatal("ServeMultiplexed did not return after closing HTTP server (timeout)")
	}

	pubHTTPS.Close()
}

// Regression guard: the main server scenario (an external httpServer is passed in, httpsServer is nil).
//
// HTTPS must use an instance independent from the external httpServer. A past regression had HTTPS reuse the same
// httpServer (i.e. util.HttpServer), causing that server's listeners to record both httpL and tlsListener -- both
// derived listeners pointing at the cmux root -- so on exit util.HttpServer.Close() would close root through them,
// m.Serve returns "use of closed network connection", and since serve.go only recognizes ErrServerClosed/
// ErrListenerClosed, it's mistakenly treated as a fatal error and os.Exit(21) fires, popping up "listen on port
// failed".
//
// This test asserts that the returned https server is a different instance from the external server passed in,
// eliminating this kind of reuse at the source.
func TestServeMultiplexed_HTTPSMustNotReuseExternalServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	handler := newTestHandler()
	certPath, keyPath := writeSelfSignedCert(t)

	// Main server mode: pass in its own httpServer, HTTPS is created internally
	externalServer := &http.Server{Handler: handler}

	type result struct {
		httpSrv  *http.Server
		httpsSrv *http.Server
		err      error
	}
	resultCh := make(chan result, 1)
	go func() {
		h, hs, e := ServeMultiplexed(ln, handler, certPath, keyPath, externalServer, nil)
		resultCh <- result{h, hs, e}
	}()

	awaitReady(t, addr)

	// Trigger the close so ServeMultiplexed returns and its return values can be checked
	externalServer.Close()

	var res result
	select {
	case res = <-resultCh:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeMultiplexed did not return after externalServer.Close() (timeout)")
	}

	// The returned http server should reuse the externally passed-in instance (main server semantics)
	if res.httpSrv != externalServer {
		t.Fatal("returned http server should be the external one")
	}
	// Key assertion: HTTPS must be an independent instance and must not reuse the external httpServer
	if res.httpsSrv == nil {
		t.Fatal("returned https server should be non-nil")
	}
	if res.httpsSrv == externalServer {
		t.Fatal("returned https server must NOT reuse the external httpServer (would close cmux root on Close)")
	}

	// Key assertion: after the external server is closed, closing the cmux-derived listener also closes root,
	// so m.Serve() subsequently returns a *net.OpError("use of closed network connection").
	// It's neither http.ErrServerClosed nor cmux.ErrListenerClosed, but it can be matched with net.ErrClosed --
	// serve.go's error-detection logic must cover this sentinel, otherwise a normal exit would be mistakenly
	// treated as a fatal error and os.Exit(21) would fire (the regression where closing any one instance among
	// multiple pops the "listen on port failed" window, see issue #18086).
	if res.err == nil {
		t.Fatal("ServeMultiplexed should return non-nil error after external server close")
	}
	if !errors.Is(res.err, net.ErrClosed) {
		t.Fatalf("returned error should match net.ErrClosed (got %v), otherwise serve.go would os.Exit(21)", res.err)
	}
}

// Regression guard: end-to-end verification of the publish service's "start-stop" loop.
// After stopping the publish service, nothing should still be listening on the port and new connections should be
// refused -- this is the precondition for the publish service being fully shut down and thus allowing a switch to
// another workspace (historical issue 16587/17973: stale connections not being dropped caused mixed-up content).
func TestServeMultiplexed_CloseDropsActiveConnections(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	handler := newTestHandler()
	certPath, keyPath := writeSelfSignedCert(t)

	pubHTTP := &http.Server{Handler: handler}
	pubHTTPS := &http.Server{Handler: handler}

	serveErrCh := make(chan error, 1)
	go func() {
		_, _, e := ServeMultiplexed(ln, handler, certPath, keyPath, pubHTTP, pubHTTPS)
		serveErrCh <- e
	}()

	awaitReady(t, addr)

	// While the service is running, it should handle HTTP requests normally
	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("request before shutdown failed: %s", err)
	}
	resp.Body.Close()

	// Stop the publish service: same order as closePublishListener -- close the listener first to stop new
	// connections, then Shutdown both servers to drop active connections, and finally Close as a fallback.
	if err := ln.Close(); err != nil {
		t.Logf("listener close: %s", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = pubHTTP.Shutdown(ctx)
	_ = pubHTTPS.Shutdown(ctx)
	cancel()
	pubHTTP.Close()
	pubHTTPS.Close()

	select {
	case <-serveErrCh:
		// ServeMultiplexed has returned
	case <-time.After(5 * time.Second):
		t.Fatal("ServeMultiplexed did not return after shutdown (timeout)")
	}

	// After shutdown, new connections should be refused (nothing is listening on the port anymore)
	if c, err := net.DialTimeout("tcp", addr, 2*time.Second); err == nil {
		c.Close()
		t.Fatal("expected connection to be refused after publish service shutdown, but dial succeeded")
	}
}

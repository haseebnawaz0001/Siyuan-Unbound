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
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/imroc/req/v3"
	"github.com/siyuan-note/httpclient"
)

const (
	maxHTTPRequestBytes     = 5 * 1024 * 1024  // Limit for text-type responses like text/html, text/plain, application/json, etc
	maxHTTPRequestFileBytes = 10 * 1024 * 1024 // Limit for saving binary responses to disk
	maxHTTPRequestChars     = 50000
)

// CheckHostSSRF verifies that the IP resolved from a hostname does not fall in an internal/loopback or otherwise
// unreachable address range, preventing the agent from being tricked into launching an SSRF attack. This check is
// shared by web_fetch and http_request.
func CheckHostSSRF(host string) error {
	ips, err := net.LookupIP(host)
	if err != nil {
		return errors.New("failed to resolve host: " + err.Error())
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate() || ip.IsUnspecified() {
			return errors.New("access to private/internal IP is prohibited")
		}
	}
	return nil
}

// HTTPRequest makes a generic HTTP call, for use by the agent's http_request tool.
// Unlike WebFetch: this function does not do HTML->Markdown conversion; text-type responses (including JSON/XML)
// are returned unchanged, so the agent can consume a REST API's JSON output directly. method takes values
// GET/POST/PUT/DELETE/PATCH.
// The returned text is either the response body (for text types) or the path of the saved file (for binary types).
func HTTPRequest(method, rawURL string, headers map[string]string, body string) (statusCode int, contentType string, text string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return 0, "", "", errors.New("URL must start with http:// or https://")
	}
	if u.Host == "" {
		return 0, "", "", errors.New("URL has no host")
	}

	if serr := CheckHostSSRF(u.Hostname()); serr != nil {
		return 0, "", "", serr
	}

	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "GET"
	}

	request := httpclient.NewBrowserRequest()
	for k, v := range headers {
		request.SetHeader(k, v)
	}
	if body != "" && method != "GET" && method != "HEAD" {
		request.SetBody(body)
	}

	resp, err := sendByMethod(request, method, rawURL)
	if err != nil {
		return 0, "", "", errors.New("request failed: " + err.Error())
	}
	if resp == nil {
		return 0, "", "", errors.New("nil response")
	}
	defer resp.Body.Close()

	statusCode = resp.StatusCode
	contentType = resp.Header.Get("Content-Type")

	maxReadBytes := int64(maxHTTPRequestBytes)
	if !isTextContentType(contentType) {
		maxReadBytes = maxHTTPRequestFileBytes
	}
	// Skip the size precheck when ContentLength is -1 (chunked), and let LimitReader truncate as a fallback.
	if resp.ContentLength > maxReadBytes {
		return statusCode, contentType, "", errors.New("response too large")
	}

	respBody, rerr := io.ReadAll(io.LimitReader(resp.Body, maxReadBytes))
	if rerr != nil {
		return statusCode, contentType, "", errors.New("read body failed: " + rerr.Error())
	}

	// Save binary responses to disk and return the file path, for the agent to further process as needed.
	if !isTextContentType(contentType) {
		importDir := filepath.Join(TempDir, "import")
		if merr := os.MkdirAll(importDir, 0755); merr != nil {
			return statusCode, contentType, "", errors.New("create import dir failed: " + merr.Error())
		}
		filename := extractFilename(rawURL, contentType)
		filePath := filepath.Join(importDir, filename)
		if werr := os.WriteFile(filePath, respBody, 0644); werr != nil {
			return statusCode, contentType, "", errors.New("write file failed: " + werr.Error())
		}
		return statusCode, contentType, fmt.Sprintf("Saved to: %s (%d bytes)", filePath, len(respBody)), nil
	}

	return statusCode, contentType, truncateRunes(string(respBody), maxHTTPRequestChars), nil
}

// sendByMethod dispatches the request by method, uniformly using the *req.Request returned by NewBrowserRequest.
func sendByMethod(request *req.Request, method, rawURL string) (*req.Response, error) {
	switch method {
	case "GET", "":
		return request.Get(rawURL)
	case "POST":
		return request.Post(rawURL)
	case "PUT":
		return request.Put(rawURL)
	case "DELETE":
		return request.Delete(rawURL)
	case "PATCH":
		return request.Patch(rawURL)
	default:
		return nil, fmt.Errorf("unsupported method: %s", method)
	}
}

// isTextContentType determines whether Content-Type is a text-type response that can be shown directly to the agent.
// Covers text/*, application/json, application/xml, application/*+json, etc.
func isTextContentType(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	if ct == "" {
		return false
	}
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	switch ct {
	case "application/json", "application/xml":
		return true
	}
	if strings.HasPrefix(ct, "application/") && (strings.HasSuffix(ct, "+json") || strings.HasSuffix(ct, "+xml")) {
		return true
	}
	return false
}

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
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/88250/gulu"
	"github.com/88250/lute/ast"
	"github.com/gin-gonic/gin"
	"github.com/imroc/req/v3"
	"github.com/siyuan-note/httpclient"
	"github.com/siyuan-note/logging"
)

var auditedAddresses sync.Map // Used to record SSRF addresses already audited, avoiding duplicate log output

// GetPrivateIPv4s gets all local private IPv4 addresses (excluding virtual network interfaces)
func GetPrivateIPv4s() (ret []string) {
	ret = []string{}

	interfaces, err := net.Interfaces()
	if err != nil {
		return
	}

	// Blacklist of common virtual network interface name keywords
	virtualKeywords := []string{"docker", "veth", "br-", "vmnet", "vbox", "utun", "tun", "tap", "bridge", "cloud", "hyper-"}

	for _, itf := range interfaces {
		// 1. Basic status filter: must be up and must not be a loopback interface
		if itf.Flags&net.FlagUp == 0 || itf.Flags&net.FlagLoopback != 0 {
			continue
		}

		// 2. Hardware address filter: a physical NIC normally must have a MAC address
		if len(itf.HardwareAddr) == 0 {
			continue
		}

		// 3. Name filter: exclude known virtual interface name prefixes
		name := strings.ToLower(itf.Name)
		isVirtual := false
		for _, kw := range virtualKeywords {
			if strings.Contains(name, kw) {
				isVirtual = true
				break
			}
		}
		if isVirtual {
			continue
		}

		// 4. Extract and validate the IP
		addrs, err := itf.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP
			// Only keep IPv4 addresses, and they must be private LAN addresses (10.x, 172.16.x, 192.168.x)
			if ip.To4() != nil && ip.IsPrivate() {
				ret = append(ret, ip.String())
			}
		}
	}
	return
}

func IsLocalHostname(hostname string) bool {
	if "localhost" == hostname || strings.HasSuffix(hostname, ".localhost") {
		return true
	}
	if ip := net.ParseIP(hostname); nil != ip {
		return ip.IsLoopback()
	}
	return false
}

func IsLocalHost(host string) bool {
	if hostname, _, err := net.SplitHostPort(strings.TrimSpace(host)); err != nil {
		return false
	} else {
		return IsLocalHostname(hostname)
	}
}

func IsLocalOrigin(origin string) bool {
	if u, err := url.Parse(origin); err == nil {
		return IsLocalHostname(u.Hostname())
	}
	return false
}

// SSRFSafeDialer returns a net.Dialer whose Control hook blocks private, loopback, link-local and unspecified IPs.
func SSRFSafeDialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{
		Timeout: timeout,
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip != nil && isPrivateIP(ip) {
				if _, loaded := auditedAddresses.LoadOrStore(address, struct{}{}); !loaded {
					logging.LogWarnf("Establishing a connection to the private network [address=%s, network=%s]", address, network)
				}
				if SafeMode {
					return fmt.Errorf("ip address [%s] is prohibited", host)
				}
			}
			return nil
		},
	}
}

func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate() || ip.IsUnspecified()
}

func IsOnline(checkURL string, skipTlsVerify bool, timeout int) bool {
	if "" == checkURL {
		return false
	}

	u, err := url.Parse(checkURL)
	if err != nil {
		logging.LogWarnf("invalid check URL [%s]", checkURL)
		return false
	}
	if u.Scheme == "file" {
		filePath := strings.TrimPrefix(checkURL, "file://")
		_, err := os.Stat(filePath)
		return err == nil
	}

	if isOnline(checkURL, skipTlsVerify, timeout) {
		return true
	}

	logging.LogWarnf("network is offline [checkURL=%s]", checkURL)
	return false
}

func IsPortOpen(port string) bool {
	timeout := time.Second
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), timeout)
	if err != nil {
		return false
	}
	if nil != conn {
		conn.Close()
		return true
	}
	return false
}

func isOnline(checkURL string, skipTlsVerify bool, timeout int) (ret bool) {
	c := req.C().
		SetTimeout(time.Duration(timeout) * time.Millisecond).
		SetProxy(httpclient.ProxyFromEnvironment).
		SetUserAgent(UserAgent)
	if skipTlsVerify {
		c.EnableInsecureSkipVerify()
	}

	for range 2 {
		resp, err := c.R().Get(checkURL)
		if resp.GetHeader("Location") != "" {
			return true
		}

		var urlErr *url.Error
		if errors.As(err, &urlErr) && urlErr.URL != checkURL {
			// DNS redirect
			logging.LogWarnf("network is online [DNS redirect, checkURL=%s, retURL=%s]", checkURL, urlErr.URL)
			return true
		}

		ret = err == nil
		if ret {
			break
		}

		logging.LogWarnf("check url [%s] is online failed: %s", checkURL, err)
		time.Sleep(1 * time.Second)
	}
	return
}

func GetRemoteAddr(req *http.Request) string {
	ret := req.Header.Get("X-forwarded-for")
	ret = strings.TrimSpace(ret)
	if "" == ret {
		ret = req.Header.Get("X-Real-IP")
	}
	ret = strings.TrimSpace(ret)
	if "" == ret {
		return req.RemoteAddr
	}
	return strings.Split(ret, ",")[0]
}

func JsonArg(c *gin.Context, result *gulu.Result) (arg map[string]any, ok bool) {
	arg = map[string]any{}
	if err := c.ShouldBindJSON(&arg); err != nil {
		result.Code = -1
		var detail string
		if errors.Is(err, io.EOF) {
			detail = "the request body is empty or truncated (EOF)"
		} else {
			detail = err.Error()
		}
		result.Msg = fmt.Sprintf("Parses request [%s] failed: %s", c.Request.URL.Path, detail)
		return
	}

	ok = true
	return
}

// GetRequestUrlStringParam extracts a string parameter from URL (path or query parameters).
func GetRequestUrlStringParam(c *gin.Context, key string) string {
	// /path/:name
	if value := c.Param(key); value != "" {
		return value
	}

	// /path?name=xxx
	if value := c.Query(key); value != "" {
		return value
	}

	return ""
}

// GetRequestStringParam extracts a string parameter from the request (URL or JSON body), with validation and error handling.
func GetRequestStringParam(c *gin.Context, key string, result *gulu.Result) string {
	// /path/:name
	if value := GetRequestUrlStringParam(c, key); value != "" {
		return value
	}

	// /path with JSON body {key: "xxx"}
	arg, ok := JsonArg(c, result)
	if !ok {
		return ""
	}
	if arg[key] == nil {
		result.Code = 1
		result.Msg = fmt.Sprintf("Request body prop [%s] does not exist", key)
		return ""
	}

	value, ok := arg[key].(string)
	if !ok {
		result.Code = 2
		result.Msg = fmt.Sprintf("Request body prop [%s] is not a string", key)
		return ""
	}
	return value
}

// ParseJsonArg uses generics to extract the value of the given key from JSON arguments.
//   - If required is true but the argument is missing, ret.Msg explains which key needs to be supplied
//   - If rejectEmpty is true but the argument value is empty, ret.Msg explains that the key must not be empty
//     (a string after trimming whitespace, an empty array, or an object with no keys)
//   - If the argument exists but its type doesn't match, ret.Msg explains the expected type for the key
//   - When the returned ok is false, it means extraction failed, the type didn't match, or the non-empty
//     constraint wasn't satisfied
func ParseJsonArg[T any](key string, arg map[string]any, ret *gulu.Result, required, rejectEmpty bool) (value T, ok bool) {
	raw, exists := arg[key]
	if !exists || raw == nil {
		if required {
			ret.Code = -1
			ret.Msg = fmt.Sprintf("Field [%s] is required", key)
		} else {
			ok = true
		}
		return
	}

	value, ok = raw.(T)
	if !ok {
		var zero T
		ret.Code = -1

		// Return the corresponding JSON type
		jsonType := ""
		switch any(zero).(type) {
		case string:
			jsonType = "String"
		case float64:
			jsonType = "Number"
		case bool:
			jsonType = "Boolean"
		case []any:
			jsonType = "Array"
		case map[string]any:
			jsonType = "Object"
		default:
			jsonType = fmt.Sprintf("%T", zero)
		}

		ret.Msg = fmt.Sprintf("Field [%s] should be of type [%s]", key, jsonType)
		return
	}

	if rejectEmpty {
		var bad bool
		switch x := any(value).(type) {
		case string:
			if t := strings.TrimSpace(x); t == "" {
				bad = true
			} else {
				value = any(t).(T)
			}
		case []any:
			bad = len(x) == 0
		case map[string]any:
			bad = len(x) == 0
		}
		if bad {
			ret.Code = -1
			ret.Msg = fmt.Sprintf("Field [%s] must not be empty", key)
			ok = false
		}
	}
	return
}

// JsonArgParseFunc is a single-field extraction function, used by ParseJsonArgs for batch extraction.
type JsonArgParseFunc func(arg map[string]any, ret *gulu.Result) bool

// BindJsonArg creates an extraction function: it takes key from arg and writes it into dest, for use with ParseJsonArgs.
func BindJsonArg[T any](key string, dest *T, required, rejectEmpty bool) JsonArgParseFunc {
	return func(arg map[string]any, ret *gulu.Result) bool {
		v, ok := ParseJsonArg[T](key, arg, ret, required, rejectEmpty)
		if !ok {
			return false
		}
		*dest = v
		return true
	}
}

// ParseJsonArgs runs multiple extraction functions in order.
//   - If any fails, returns false and writes the error message into ret
//   - Returns true if all succeed
func ParseJsonArgs(arg map[string]any, ret *gulu.Result, extractors ...JsonArgParseFunc) bool {
	for _, ext := range extractors {
		if !ext(arg, ret) {
			return false
		}
	}
	return true
}

func InvalidIDPattern(idArg string, result *gulu.Result) bool {
	if ast.IsNodeIDPattern(idArg) {
		return false
	}

	result.Code = -1
	result.Msg = "invalid ID argument"
	return true
}

func initHttpClient() {
	http.DefaultClient = httpclient.GetCloudFileClient2Min()
	http.DefaultTransport = httpclient.NewTransport(false)
}

func ParsePort(portString string) (uint16, error) {
	port, err := strconv.ParseUint(portString, 10, 16)
	if err != nil {
		logging.LogErrorf("parse port [%s] failed: %s", portString, err)
		return 0, err
	}
	return uint16(port), nil
}

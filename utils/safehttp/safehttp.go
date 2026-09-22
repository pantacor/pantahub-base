// Copyright (c) 2026 Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.
//

// Package safehttp fetches URLs chosen by somebody else without letting them
// aim this server at itself or its network: connections go only to publicly
// routable addresses, checked on the address actually dialled, after name
// resolution, so a hostname cannot resolve or be re-pointed to something
// internal between a check and the connection.
package safehttp

import (
	"errors"
	"net"
	"net/http"
	"syscall"
	"time"
)

// ErrBlockedAddress means a connection was refused because its address is
// not publicly routable.
var ErrBlockedAddress = errors.New("the address is not publicly routable")

// PublicAddress reports whether ip is safe to connect to on behalf of a
// stranger: globally routable unicast, nothing else.
func PublicAddress(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	for _, block := range blockedNetworks {
		if block.Contains(ip) {
			return false
		}
	}
	return true
}

// blockedNetworks are the ranges net.IP's own predicates do not cover.
var blockedNetworks = mustParseCIDRs(
	"0.0.0.0/8",       // "this network"
	"100.64.0.0/10",   // carrier grade NAT, used by cloud and tailnet internals
	"192.0.0.0/24",    // IETF protocol assignments
	"192.0.2.0/24",    // documentation
	"198.18.0.0/15",   // benchmarking
	"198.51.100.0/24", // documentation
	"203.0.113.0/24",  // documentation
	"240.0.0.0/4",     // reserved
	"64:ff9b::/96",    // NAT64, which can map onto private IPv4
	"2001:db8::/32",   // documentation
)

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	networks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(err)
		}
		networks = append(networks, network)
	}
	return networks
}

// DialControl refuses a connection to anything that is not a public address.
// It is a net.Dialer Control function.
func DialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if !PublicAddress(net.ParseIP(host)) {
		return ErrBlockedAddress
	}
	return nil
}

// Options shape a client.
type Options struct {
	// Timeout bounds the whole request, the body included.
	Timeout time.Duration
	// MaxResponseHeaderBytes bounds the response headers.
	MaxResponseHeaderBytes int64
	// CheckRedirect decides on redirects, as http.Client.CheckRedirect does.
	// Nil refuses every redirect: the response is the one of the URL asked.
	CheckRedirect func(req *http.Request, via []*http.Request) error
}

// NewClient returns a client that only connects to public addresses and uses
// no proxy, which would make the connection for it and leave the address
// check only ever seeing the proxy.
func NewClient(opts Options) *http.Client {
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.MaxResponseHeaderBytes <= 0 {
		opts.MaxResponseHeaderBytes = 16 << 10
	}
	checkRedirect := opts.CheckRedirect
	if checkRedirect == nil {
		checkRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}

	connectTimeout := opts.Timeout
	if connectTimeout > 30*time.Second {
		connectTimeout = 30 * time.Second
	}
	dialer := &net.Dialer{Timeout: connectTimeout, Control: DialControl}
	return &http.Client{
		Timeout: opts.Timeout,
		Transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            dialer.DialContext,
			TLSHandshakeTimeout:    connectTimeout,
			ResponseHeaderTimeout:  connectTimeout,
			MaxResponseHeaderBytes: opts.MaxResponseHeaderBytes,
			DisableKeepAlives:      true,
		},
		CheckRedirect: checkRedirect,
	}
}

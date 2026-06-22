package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"time"
)

// probeTLS dials host:port with TLS (InsecureSkipVerify so self-signed certs
// are accepted) and returns the leaf certificate's expiry, subject CN, and
// issuer CN. The dial times out after 1.5 s so it fits within CollectAll's
// 2 s budget alongside process-metric and disk collection.
func probeTLS(host, port string) (expiry time.Time, subject, issuer string, err error) {
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 1500 * time.Millisecond},
		"tcp",
		net.JoinHostPort(host, port),
		&tls.Config{InsecureSkipVerify: true}, //nolint:gosec // intentional — we probe, not verify
	)
	if err != nil {
		return time.Time{}, "", "", err
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return time.Time{}, "", "", fmt.Errorf("no certificates presented")
	}
	leaf := certs[0]
	return leaf.NotAfter, leaf.Subject.CommonName, leaf.Issuer.CommonName, nil
}

// tlsDetail is the full certificate/handshake breakdown shown in the Info Panel.
type tlsDetail struct {
	Probed     bool
	Err        string
	Version    string
	Cipher     string
	Subject    string
	Issuer     string
	NotBefore  time.Time
	NotAfter   time.Time
	SANs       []string
	SelfSigned bool
}

// probeTLSDetail performs a full handshake against host:port and extracts the
// negotiated version/cipher plus the leaf certificate's identity, validity
// window, and subject alternative names. It always marks Probed=true so the
// Info Panel can distinguish "probed but failed" from "not a TLS service".
func probeTLSDetail(host, port string) tlsDetail {
	d := tlsDetail{Probed: true}
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 3 * time.Second},
		"tcp",
		net.JoinHostPort(host, port),
		&tls.Config{InsecureSkipVerify: true}, //nolint:gosec // intentional — we probe, not verify
	)
	if err != nil {
		d.Err = err.Error()
		return d
	}
	defer conn.Close()

	state := conn.ConnectionState()
	d.Version = tls.VersionName(state.Version)
	d.Cipher = tls.CipherSuiteName(state.CipherSuite)

	if len(state.PeerCertificates) == 0 {
		d.Err = "no certificates presented"
		return d
	}
	leaf := state.PeerCertificates[0]
	d.Subject = leaf.Subject.CommonName
	d.Issuer = leaf.Issuer.CommonName
	d.NotBefore = leaf.NotBefore
	d.NotAfter = leaf.NotAfter
	d.SANs = leaf.DNSNames
	d.SelfSigned = leaf.Subject.String() == leaf.Issuer.String()
	return d
}

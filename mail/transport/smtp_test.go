package transport_test

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/mail"
	"github.com/arandu-io/hesape/mail/transport"
)

// selfSigned is a certificate for 127.0.0.1, and the pool that trusts it.
func selfSigned(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "smtp.test"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

// session is what one SMTP conversation delivered to the test server.
type session struct {
	// tls says whether the connection was encrypted when DATA arrived.
	tls bool
	// user is who authenticated, empty when nobody did.
	user string
	// data is the message body, empty when the session ended before DATA.
	data string
}

// smtpServer answers one SMTP session on l. X// STARTTLS with that configuration; it accepts AUTH PLAIN whatever the credentials.
func smtpServer(t *testing.T, l net.Listener, startTLS *tls.Config) <-chan session {
	t.Helper()
	got := make(chan session, 1)
	go func() {
		defer close(got)
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

		var s session
		_, s.tls = conn.(*tls.Conn)
		r := bufio.NewReader(conn)
		write := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
		write("220 smtp.test ESMTP")
		var data strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				s.data = data.String()
				got <- s
				return
			}
			cmd := strings.TrimSpace(line)
			upper := strings.ToUpper(cmd)
			switch {
			case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
				if startTLS != nil && !s.tls {
					write("250-smtp.test")
					write("250-STARTTLS")
					write("250 AUTH PLAIN")
				} else {
					write("250-smtp.test")
					write("250 AUTH PLAIN")
				}
			case upper == "STARTTLS" && startTLS != nil && !s.tls:
				write("220 go ahead")
				secured := tls.Server(conn, startTLS)
				if err := secured.Handshake(); err != nil {
					got <- s
					return
				}
				conn, s.tls = secured, true
				r = bufio.NewReader(conn)
			case strings.HasPrefix(upper, "AUTH PLAIN"):
				raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(cmd[len("AUTH PLAIN"):]))
				if parts := strings.Split(string(raw), "\x00"); len(parts) == 3 {
					s.user = parts[1]
				}
				write("235 accepted")
			case strings.HasPrefix(upper, "MAIL"), strings.HasPrefix(upper, "RCPT"):
				write("250 ok")
			case upper == "DATA":
				write("354 go ahead")
				for {
					l, err := r.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
					data.WriteString(l)
				}
				write("250 queued")
			case upper == "QUIT":
				write("221 bye")
				s.data = data.String()
				got <- s
				return
			default:
				write("502 not implemented")
			}
		}
	}()
	return got
}

func receipt() mail.Message {
	return mail.Message{
		Envelope: mail.Envelope{
			From:    mail.Address{Address: "app@example.test"},
			To:      []mail.Address{{Address: "ana@example.test"}},
			Subject: "Receipt",
		},
		Text: "Paid in full.",
	}
}

func hostPort(t *testing.T, l net.Listener) (string, string) {
	t.Helper()
	host, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}

// TestSMTPSpeaksImplicitTLSWhenTheConfigurationSaysSo: with Encryption "tls"
// -- what smtps:// means -- the handshake completes before the greeting, the
// password goes over it, and the message arrives encrypted.
func TestSMTPSpeaksImplicitTLSWhenTheConfigurationSaysSo(t *testing.T) {
	cert, pool := selfSigned(t)
	l, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	got := smtpServer(t, l, nil)

	host, port := hostPort(t, l)
	tr := transport.SMTP{Host: host, Port: port, Username: "ana", Password: "secret",
		Encryption: transport.EncryptionTLS, Timeout: 5 * time.Second, TLSConfig: &tls.Config{RootCAs: pool}}
	if _, err := tr.Send(context.Background(), receipt()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	s := <-got
	if !s.tls || s.user != "ana" || !strings.Contains(s.data, "Paid in full.") {
		t.Fatalf("the server saw tls=%v user=%q data=%q; want the message, authenticated, over TLS", s.tls, s.user, s.data)
	}
}

// TestSMTPUpgradesWithStartTLSAgainstTheConfiguredAuthority: STARTTLS is
// verified with the same TLSConfig, so a relay with a private authority works
// on 587 as well as on 465.
func TestSMTPUpgradesWithStartTLSAgainstTheConfiguredAuthority(t *testing.T) {
	cert, pool := selfSigned(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	got := smtpServer(t, l, &tls.Config{Certificates: []tls.Certificate{cert}})

	host, port := hostPort(t, l)
	tr := transport.SMTP{Host: host, Port: port, Username: "ana", Password: "secret",
		Encryption: transport.EncryptionStartTLS, Timeout: 5 * time.Second, TLSConfig: &tls.Config{RootCAs: pool}}
	if _, err := tr.Send(context.Background(), receipt()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	s := <-got
	if !s.tls || s.user != "ana" || !strings.Contains(s.data, "Paid in full.") {
		t.Fatalf("the server saw tls=%v user=%q data=%q; want the message, authenticated, after STARTTLS", s.tls, s.user, s.data)
	}
}

// TestSMTPRefusesAServerItCannotVerify: implicit TLS against a certificate
// nobody vouched for sends nothing.
func TestSMTPRefusesAServerItCannotVerify(t *testing.T) {
	cert, _ := selfSigned(t)
	l, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	got := smtpServer(t, l, nil)

	host, port := hostPort(t, l)
	tr := transport.SMTP{Host: host, Port: port, Encryption: transport.EncryptionTLS, Timeout: 5 * time.Second}
	if _, err := tr.Send(context.Background(), receipt()); err == nil || !strings.Contains(err.Error(), "tls") {
		t.Fatalf("Send = %v, want the handshake refused", err)
	}
	_ = l.Close()
	if s := <-got; s.data != "" {
		t.Fatalf("the server received %q from a client that could not verify it", s.data)
	}
}

// TestSMTPWithImplicitTLSSendsNothingToAPlainServer: a server that speaks SMTP
// in the clear is never greeted, because the handshake comes first.
func TestSMTPWithImplicitTLSSendsNothingToAPlainServer(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	got := smtpServer(t, l, nil)

	host, port := hostPort(t, l)
	tr := transport.SMTP{Host: host, Port: port, Encryption: transport.EncryptionTLS, Timeout: 2 * time.Second}
	if _, err := tr.Send(context.Background(), receipt()); err == nil {
		t.Fatal("Send succeeded over a connection that was never encrypted")
	}
	_ = l.Close()
	if s := <-got; strings.Contains(s.data, "Paid in full.") {
		t.Fatal("the message went out in the clear")
	}
}

// TestSMTPRefusesAnEncryptionItDoesNotKnow: a misspelling is an error before
// anything is dialled, never a plain session. Port 1 is closed, so an attempt
// to dial would answer with a different error.
func TestSMTPRefusesAnEncryptionItDoesNotKnow(t *testing.T) {
	_, err := transport.SMTP{Host: "127.0.0.1", Port: "1", Encryption: "tsl"}.Send(context.Background(), receipt())
	if err == nil || !strings.Contains(err.Error(), `"tsl"`) {
		t.Fatalf("Send = %v, want the unknown encryption named", err)
	}
}

// TestSMTPWithoutEncryptionStillSendsToARelayWithoutStartTLS: the empty value
// keeps what the transport always did against a local relay that offers no
// STARTTLS.
func TestSMTPWithoutEncryptionStillSendsToARelayWithoutStartTLS(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	got := smtpServer(t, l, nil)

	host, port := hostPort(t, l)
	if _, err := (transport.SMTP{Host: host, Port: port, Timeout: 5 * time.Second}).Send(context.Background(), receipt()); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if s := <-got; s.tls || !strings.Contains(s.data, "Paid in full.") {
		t.Fatalf("the server saw tls=%v data=%q; want the message in the clear", s.tls, s.data)
	}
}

// TestTheSchemeOfAMailerChoosesTheEncryption: smtps is implicit TLS on 465,
// smtp or none is STARTTLS on 587, a port set by hand wins, and any other
// scheme is refused when the transport is built.
func TestTheSchemeOfAMailerChoosesTheEncryption(t *testing.T) {
	manager := transport.Register(mail.NewMailManager(mail.ManagerConfig{}, nil, nil))
	cases := []struct {
		scheme, port         string
		encryption, wantPort string
	}{
		{"smtps", "", transport.EncryptionTLS, "465"},
		{"SMTPS", "2465", transport.EncryptionTLS, "2465"},
		{"smtp", "", transport.EncryptionStartTLS, "587"},
		{"", "", transport.EncryptionStartTLS, "587"},
		{"", "1025", transport.EncryptionStartTLS, "1025"},
	}
	for _, tc := range cases {
		built, err := manager.CreateTransport(mail.MailerConfig{Name: "main", Transport: "smtp", Host: "smtp.example.test", Scheme: tc.scheme, Port: tc.port})
		if err != nil {
			t.Fatalf("scheme %q: %v", tc.scheme, err)
		}
		s, ok := built.(transport.SMTP)
		if !ok {
			t.Fatalf("scheme %q built %T, want transport.SMTP", tc.scheme, built)
		}
		if s.Encryption != tc.encryption || s.Port != tc.wantPort {
			t.Errorf("scheme %q port %q: Encryption %q on %s, want %q on %s", tc.scheme, tc.port, s.Encryption, s.Port, tc.encryption, tc.wantPort)
		}
	}

	if _, err := manager.CreateTransport(mail.MailerConfig{Name: "main", Transport: "smtp", Scheme: "smtpx"}); err == nil || !strings.Contains(err.Error(), `"smtpx"`) {
		t.Fatalf("CreateTransport with scheme smtpx = %v, want the scheme refused by name", err)
	}
}

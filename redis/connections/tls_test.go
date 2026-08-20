// The tests below run without REDIS_ADDRESS, because they are about the
// transport and not about the protocol: the server they talk to answers PING
// and nothing else, which is all a handshake needs to be proven to have
// happened.

package connections_test

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/redis/connections"
	goredis "github.com/redis/go-redis/v9"
)

const testServerName = "resp.example"

// TestConnectDialsInTheClearByDefault: TLS is a field nobody has to set, and an
// option nobody set may not change how the connection dials.
func TestConnectDialsInTheClearByDefault(t *testing.T) {
	conn := connections.Connect(connections.Config{Address: "127.0.0.1:6379"})
	t.Cleanup(func() { _ = conn.Close() })

	if config := configuredTLS(t, conn); config != nil {
		t.Errorf("the driver was handed a TLS configuration (%v) for a connection that asked for none", config)
	}
}

// TestConnectHandsTheTLSConfigurationToTheDriver: the field is worth nothing if
// it stops at the struct.
func TestConnectHandsTheTLSConfigurationToTheDriver(t *testing.T) {
	config := &tls.Config{MinVersion: tls.VersionTLS13}
	conn := connections.Connect(connections.Config{Address: "127.0.0.1:6379", TLS: config})
	t.Cleanup(func() { _ = conn.Close() })

	if got := configuredTLS(t, conn); got != config {
		t.Errorf("the driver was handed %v, want the configuration Connect received", got)
	}
}

// TestEveryClusterNodeGetsTheConfiguration: a sharded deployment is several
// addresses and one client, and encryption that reached only the first of them
// would be a connection half in the clear.
func TestEveryClusterNodeGetsTheConfiguration(t *testing.T) {
	config := &tls.Config{MinVersion: tls.VersionTLS13}
	conn := connections.Connect(connections.Config{
		Addresses: []string{"127.0.0.1:7000", "127.0.0.1:7001"},
		TLS:       config,
	})
	t.Cleanup(func() { _ = conn.Close() })

	if got := configuredTLS(t, conn); got != config {
		t.Errorf("the cluster client was handed %v, want the configuration Connect received", got)
	}
}

// TestPingSucceedsOverTLSSignedByAPrivateAuthority is the case the field exists
// for. A self-hosted server presents a certificate no public authority signed,
// so trusting it means naming the root, and naming the root is why the field
// carries a whole tls.Config instead of a flag.
func TestPingSucceedsOverTLSSignedByAPrivateAuthority(t *testing.T) {
	serverCertificate, roots := certificate(t)
	address := startPingServer(t, serverConfig(serverCertificate))

	conn := connections.Connect(connections.Config{
		Address: address,
		TLS:     &tls.Config{RootCAs: roots, ServerName: testServerName, MinVersion: tls.VersionTLS12},
	})
	t.Cleanup(func() { _ = conn.Close() })

	if err := conn.Ping(context.Background()); err != nil {
		t.Fatalf("Ping over TLS: %v", err)
	}
}

// TestPingFailsWithoutTLSAgainstAServerThatSpeaksIt is the negative half of the
// test above: the same connection reaches the same server, and without the
// field it gets nowhere. That is what says the handshake was the difference,
// rather than a server that would have answered either way.
func TestPingFailsWithoutTLSAgainstAServerThatSpeaksIt(t *testing.T) {
	serverCertificate, _ := certificate(t)
	address := startPingServer(t, serverConfig(serverCertificate))

	conn := connections.Connect(connections.Config{
		Address:     address,
		DialTimeout: 2 * time.Second,
		ReadTimeout: 2 * time.Second,
	})
	t.Cleanup(func() { _ = conn.Close() })

	if err := conn.Ping(context.Background()); err == nil {
		t.Error("Ping reported success against a server it never handshook with")
	}
}

// TestTheCertificateIsVerified: the connection is encrypted and the peer is
// checked, which are two claims and not one. A client that skipped verification
// would still pass the test above while accepting whoever answered the address.
func TestTheCertificateIsVerified(t *testing.T) {
	serverCertificate, _ := certificate(t)
	address := startPingServer(t, serverConfig(serverCertificate))

	// The system roots, which signed nothing here.
	conn := connections.Connect(connections.Config{
		Address:     address,
		DialTimeout: 2 * time.Second,
		ReadTimeout: 2 * time.Second,
		TLS:         &tls.Config{ServerName: testServerName, MinVersion: tls.VersionTLS12},
	})
	t.Cleanup(func() { _ = conn.Close() })

	if err := conn.Ping(context.Background()); err == nil {
		t.Error("Ping accepted a certificate signed by an authority it was never told to trust")
	}
}

// configuredTLS reads back what the driver was actually handed. One address is
// the single-node client and several are the cluster client, and the two carry
// the setting on options types of their own.
func configuredTLS(t *testing.T, conn *connections.Connection) *tls.Config {
	t.Helper()

	switch client := conn.Client().(type) {
	case *goredis.Client:
		return client.Options().TLSConfig
	case *goredis.ClusterClient:
		return client.Options().TLSConfig
	default:
		t.Fatalf("the connection holds a %T, which this test cannot read the transport of", client)
		return nil
	}
}

// serverConfig is the server half of the handshake.
func serverConfig(c tls.Certificate) *tls.Config {
	return &tls.Config{Certificates: []tls.Certificate{c}, MinVersion: tls.VersionTLS12}
}

// certificate mints a certificate for testServerName, signed by a root that
// nothing on the machine trusts, and returns it with the pool that does.
func certificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: testServerName},
		DNSNames:              []string{testServerName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("signing a certificate: %v", err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing the certificate back: %v", err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: parsed}, roots
}

// startPingServer listens on a loopback port behind the TLS configuration given
// and returns its address. It is not a key-value store: it answers PING and
// refuses everything else, which is what makes a connection that completes
// distinguishable from one that does not.
func startPingServer(t *testing.T, config *tls.Config) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	address := listener.Addr().String()
	listener = tls.NewListener(listener, config)
	t.Cleanup(func() { _ = listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go servePing(conn)
		}
	}()
	return address
}

// servePing answers PING and reports every other command as unknown, which a
// client treats as a server too old for it rather than as a failure: that is
// the path a driver takes when its opening handshake is refused, and taking it
// here keeps the fake to one command.
func servePing(conn net.Conn) {
	defer func() { _ = conn.Close() }()

	reader := bufio.NewReader(conn)
	for {
		name, err := readCommandName(reader)
		if err != nil {
			return
		}
		reply := "-ERR unknown command\r\n"
		if strings.EqualFold(name, "ping") {
			reply = "+PONG\r\n"
		}
		if _, err := io.WriteString(conn, reply); err != nil {
			return
		}
	}
}

// readCommandName consumes one RESP array of bulk strings and returns the first
// of them, which is the command.
func readCommandName(reader *bufio.Reader) (string, error) {
	count, err := readPrefixedInt(reader, '*')
	if err != nil {
		return "", err
	}

	name := ""
	for i := 0; i < count; i++ {
		size, err := readPrefixedInt(reader, '$')
		if err != nil {
			return "", err
		}
		if size < 0 {
			return "", fmt.Errorf("a request carried an argument of length %d", size)
		}
		argument := make([]byte, size+len("\r\n"))
		if _, err := io.ReadFull(reader, argument); err != nil {
			return "", err
		}
		if i == 0 {
			name = string(argument[:size])
		}
	}
	return name, nil
}

// readPrefixedInt reads one RESP length line: the prefix, the number, CRLF.
func readPrefixedInt(reader *bufio.Reader, prefix byte) (int, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return 0, err
	}
	if len(line) < len("*0\r\n") || line[0] != prefix {
		return 0, fmt.Errorf("want a line opening with %q, got %q", prefix, line)
	}
	return strconv.Atoi(strings.TrimSuffix(line[1:], "\r\n"))
}

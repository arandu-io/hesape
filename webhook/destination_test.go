package webhook

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"

	httpclient "github.com/arandu-io/hesape/http/client"
)

// failingResolver is a name server that does not answer.
type failingResolver struct{}

func (failingResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return nil, errors.New("no such host")
}

// byName answers each host with its own addresses.
type byName map[string][]netip.Addr

func (r byName) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if addrs, ok := r[host]; ok {
		return addrs, nil
	}
	return nil, errors.New("no such host")
}

// dialRecorder counts the connections a dialer was allowed to make.
func dialRecorder(count *int) func(context.Context, string, string) (net.Conn, error) {
	return func(context.Context, string, string) (net.Conn, error) {
		*count++
		return nil, errors.New("dialed")
	}
}

func TestCheckDestinationRefusesEveryAddressInsideTheNetwork(t *testing.T) {
	for _, address := range []string{
		"https://localhost/hook", "https://LOCALHOST./hook", "https://api.localhost/hook",
		"https://127.0.0.1/hook", "https://127.1.2.3:8443/hook", "https://10.0.0.5/hook", "https://172.16.0.1/hook",
		"https://192.168.1.1/hook", "https://169.254.169.254/latest/meta-data", "https://100.64.0.1/hook",
		"https://100.127.255.254/hook", "https://0.0.0.0/hook", "https://0.1.2.3/hook", "https://198.18.0.1/hook",
		"https://240.0.0.1/hook", "https://224.0.0.1/hook", "https://255.255.255.255/hook",
		"https://[::1]/hook", "https://[::]/hook", "https://[fe80::1]/hook", "https://[fe80::1%25en0]/hook",
		"https://[fc00::1]/hook", "https://[fd12:3456::1]/hook", "https://[fec0::1]/hook", "https://[ff02::1]/hook",
		"https://[::ffff:127.0.0.1]/hook", "https://[::ffff:10.0.0.5]/hook", "https://[::ffff:169.254.169.254]/hook",
		// An IPv4 address carried inside an IPv6 one that a translating network reaches.
		"https://[64:ff9b::7f00:1]/hook", "https://[64:ff9b::a9fe:a9fe]/hook", "https://[2002:7f00:1::]/hook",
		"https://[2002:a00:5::1]/hook", "https://[::7f00:1]/hook",
	} {
		err := CheckDestination(context.Background(), address, failingResolver{})
		if !errors.Is(err, httpclient.ErrInternalAddress) {
			t.Errorf("CheckDestination(%s) = %v, want ErrInternalAddress", address, err)
		}
	}
}

func TestCheckDestinationAcceptsAPublicAddress(t *testing.T) {
	for _, address := range []string{
		"https://93.184.216.34/hook", "https://[2606:2800:220:1:248:1893:25c8:1946]/hook", "https://hooks.example.com:8443/a?b=c",
		"https://[64:ff9b::5db8:d822]/hook",
	} {
		if err := CheckDestination(context.Background(), address, failingResolver{}); err != nil {
			t.Errorf("CheckDestination(%s) = %v, want nil", address, err)
		}
	}
}

func TestCheckDestinationRefusesANameThatResolvesInsideTheNetwork(t *testing.T) {
	names := byName{
		"rebind.example.com": {netip.MustParseAddr("127.0.0.1")},
		"mixed.example.com":  {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.5")},
		"v6.example.com":     {netip.MustParseAddr("::1")},
		"mapped.example.com": {netip.MustParseAddr("::ffff:169.254.169.254")},
		"public.example.com": {netip.MustParseAddr("93.184.216.34")},
	}
	for _, host := range []string{"rebind", "mixed", "v6", "mapped"} {
		err := CheckDestination(context.Background(), "https://"+host+".example.com/hook", names)
		if !errors.Is(err, httpclient.ErrInternalAddress) {
			t.Errorf("%s: CheckDestination = %v, want ErrInternalAddress", host, err)
		}
	}
	if err := CheckDestination(context.Background(), "https://public.example.com/hook", names); err != nil {
		t.Errorf("a public name = %v, want nil", err)
	}
}

// A name that does not resolve yet may be registered: the merchant publishes
// the record after the endpoint, and every attempt resolves it again.
func TestCheckDestinationAcceptsANameThatDoesNotResolveYet(t *testing.T) {
	if err := CheckDestination(context.Background(), "https://later.example.com/hook", failingResolver{}); err != nil {
		t.Fatalf("CheckDestination = %v, want nil", err)
	}
}

func TestCheckDestinationRefusesWhatNormalizeURLRefuses(t *testing.T) {
	long := "https://example.com/" + strings.Repeat("a", MaxURLLength)
	for _, address := range []string{"", "not a url", "ftp://example.com/x", "https://user:pw@example.com/x", "https:///path", long} {
		if err := CheckDestination(context.Background(), address, failingResolver{}); !errors.Is(err, ErrInvalidURL) {
			t.Errorf("CheckDestination(%.40q) = %v, want ErrInvalidURL", address, err)
		}
	}
	// The longest address the column holds is accepted.
	exact := "https://example.com/" + strings.Repeat("a", MaxURLLength-len("https://example.com/"))
	if err := CheckDestination(context.Background(), exact, failingResolver{}); err != nil {
		t.Errorf("an address of exactly %d bytes = %v, want nil", MaxURLLength, err)
	}
}

// The delivery's dialer holds the address it resolved, so a name that answered
// a public address when it was stored and an internal one when it is called
// reaches nothing.
func TestTheDialerRefusesAStoredNameThatNowResolvesInsideTheNetwork(t *testing.T) {
	stored := byName{"hooks.example.com": {netip.MustParseAddr("93.184.216.34")}}
	if err := CheckDestination(context.Background(), "https://hooks.example.com/hook", stored); err != nil {
		t.Fatalf("the name was refused when it was stored: %v", err)
	}
	dialed := 0
	next := dialRecorder(&dialed)
	rebound := publicDialer(next, byName{"hooks.example.com": {netip.MustParseAddr("169.254.169.254")}})
	if _, err := rebound(context.Background(), "tcp", "hooks.example.com:443"); !errors.Is(err, httpclient.ErrInternalAddress) {
		t.Fatalf("the rebound name = %v, want ErrInternalAddress", err)
	}
	if dialed != 0 {
		t.Fatalf("the rebound name was dialed %d times", dialed)
	}
}

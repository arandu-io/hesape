package mail

import "net/mail"

// Address is Illuminate\Mail\Mailables\Address: one mailbox, with the display
// name that goes in front of it.
type Address struct {
	// Address is the address itself, and the only required half. It carries
	// Illuminate's property name, which is why the field repeats the type.
	Address string
	// Name is what a client shows instead of the address. Empty is fine.
	Name string
}

// NewAddress is Illuminate's Address::__construct, whose second argument is
// optional and arrives here as a variadic tail.
func NewAddress(address string, name ...string) Address {
	a := Address{Address: address}
	if len(name) > 0 {
		a.Name = name[0]
	}
	return a
}

// String renders the address the way a header carries it.
func (a Address) String() string {
	if a.Name == "" {
		return a.Address
	}
	return (&mail.Address{Name: a.Name, Address: a.Address}).String()
}

// Valid reports whether the address parses. It is checked before a transport is
// asked to do anything, so a typo fails at the call rather than as a bounce
// three minutes later.
func (a Address) Valid() bool {
	if a.Address == "" {
		return false
	}
	_, err := mail.ParseAddress(a.Address)
	return err == nil
}

// addresses turns whatever Illuminate accepts for a recipient into the list it
// stores. PHP's setAddress takes string, array, Collection, Symfony Address or
// Mailables\Address; the Go equivalent of that "mixed" is any, and the shapes
// below are the ones that arrive.
func addressesOf(address any, name ...string) []Address {
	only := ""
	if len(name) > 0 {
		only = name[0]
	}

	switch v := address.(type) {
	case nil:
		return nil
	case string:
		if v == "" {
			return nil
		}
		return []Address{{Address: v, Name: only}}
	case Address:
		return []Address{v}
	case *Address:
		if v == nil {
			return nil
		}
		return []Address{*v}
	case []string:
		out := make([]Address, 0, len(v))
		for _, s := range v {
			out = append(out, Address{Address: s})
		}
		return out
	case []Address:
		return append([]Address(nil), v...)
	case []*Address:
		out := make([]Address, 0, len(v))
		for _, p := range v {
			if p != nil {
				out = append(out, *p)
			}
		}
		return out
	case map[string]string:
		// Illuminate's ['taylor@laravel.com' => 'Taylor'] shape. A Go map has no
		// order, so the result is sorted by address to stay reproducible --
		// PHP's array is ordered and this is the one place the two differ.
		out := make([]Address, 0, len(v))
		for addr, n := range v {
			out = append(out, Address{Address: addr, Name: n})
		}
		sortAddresses(out)
		return out
	default:
		return nil
	}
}

func sortAddresses(list []Address) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].Address < list[j-1].Address; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// uniqueAddresses is Illuminate's
// (new Collection($this->{$property}))->reverse()->unique('address')->reverse().
//
// The reverse-unique-reverse keeps the *last* spelling of a repeated address,
// which is what makes to('a@b') then to('a@b', 'Ada') end with the name
// attached. Getting this backwards is invisible until somebody complains that
// the display name went missing.
func uniqueAddresses(list []Address) []Address {
	seen := make(map[string]int, len(list))
	for i, a := range list {
		seen[a.Address] = i
	}
	out := make([]Address, 0, len(list))
	for i, a := range list {
		if seen[a.Address] == i {
			out = append(out, a)
		}
	}
	return out
}

// hasAddress is Illuminate's hasRecipient: a name of nil matches on the address
// alone, and a name given matches on both.
func hasAddress(list []Address, address string, name ...string) bool {
	for _, a := range list {
		if a.Address != address {
			continue
		}
		if len(name) == 0 {
			return true
		}
		if a.Name == name[0] {
			return true
		}
	}
	return false
}

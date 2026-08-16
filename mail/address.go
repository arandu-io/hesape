package mail

import "net/mail"

// Address is one mailbox, with the display name that goes in front of it.
type Address struct {
	// Address is the address itself, and the only required half.
	Address string
	// Name is what a client shows instead of the address. Empty is fine.
	Name string
}

// NewAddress builds an address. The display name is optional.
func NewAddress(address string, name ...string) Address {
	a := Address{Address: address}
	if len(name) > 0 {
		a.Name = name[0]
	}
	return a
}

// String renders the address in the RFC 5322 form a header carries: the
// address alone when there is no display name, and the name in front of it when
// there is.
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

// addressesOf turns whatever a caller may pass for a recipient into the list
// that is stored. The parameter is an any because the accepted shapes have no
// type in common; they are the cases below, and anything else is nothing.
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
		// A map of address to display name. A Go map has no order, so the
		// result is sorted by address to stay reproducible.
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

// uniqueAddresses drops repeated addresses, keeping the *last* spelling of each
// where its last spelling was. That is what makes To("a@b") then To("a@b",
// "Ada") end with the name attached. Getting it backwards is invisible until
// somebody complains that the display name went missing.
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

// hasAddress looks an address up in a list: no name matches on the address
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

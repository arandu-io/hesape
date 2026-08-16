package number

import "strings"

// currencySymbols holds the currencies whose symbol a reader of en-US is
// expected to recognise. A code that is not here is printed as itself, so the
// table can stay short and correct instead of long and half wrong.
var currencySymbols = map[string]string{
	"AUD": "A$",
	"BRL": "R$",
	"CAD": "CA$",
	"CNY": "CN¥",
	"EUR": "€",
	"GBP": "£",
	"ILS": "₪",
	"INR": "₹",
	"JPY": "¥",
	"KRW": "₩",
	"MXN": "MX$",
	"NGN": "₦",
	"PHP": "₱",
	"RUB": "₽",
	"THB": "฿",
	"TRY": "₺",
	"USD": "$",
	"VND": "₫",
}

// currencyDigits holds the ISO 4217 currencies that do not have two minor
// units. Anything absent from this table is rendered with two.
var currencyDigits = map[string]int{
	"BHD": 3,
	"BIF": 0,
	"CLP": 0,
	"DJF": 0,
	"GNF": 0,
	"IQD": 3,
	"ISK": 0,
	"JOD": 3,
	"JPY": 0,
	"KMF": 0,
	"KRW": 0,
	"KWD": 3,
	"LYD": 3,
	"OMR": 3,
	"PYG": 0,
	"RWF": 0,
	"TND": 3,
	"UGX": 0,
	"VND": 0,
	"VUV": 0,
	"XAF": 0,
	"XOF": 0,
	"XPF": 0,
}

// Currency renders v as an amount in the given ISO 4217 currency. The code is
// read case-insensitively and decides two things: the symbol, and how many
// digits the currency keeps after the decimal point — two for most, none for
// the yen, three for the dinars.
//
// An empty currency falls back to DefaultCurrency. The optional argument is a
// precision, which overrides the digit count the currency itself carries.
//
// A code with no symbol in the table is printed in front of the amount as it
// was given. The sign leads, as it does in en-US.
//
//	Currency(1234.5, "USD")  // "$1,234.50"
//	Currency(1234.56, "JPY") // "¥1,235"
//	Currency(-99, "CHF")     // "-CHF 99.00"
//	Currency(1234.5, "USD", 0) // "$1,235"
func Currency(v float64, in string, precision ...int) string {
	if s, ok := special(v); ok {
		return s
	}
	code := strings.ToUpper(strings.TrimSpace(in))
	if code == "" {
		code = strings.ToUpper(strings.TrimSpace(DefaultCurrency()))
	}

	digits := 2
	if d, ok := currencyDigits[code]; ok {
		digits = d
	}
	if len(precision) > 0 {
		digits = max(precision[0], 0)
	}

	amount := Format(v, digits)
	sign := ""
	if strings.HasPrefix(amount, "-") {
		sign, amount = "-", amount[1:]
	}

	switch {
	case code == "":
		return sign + amount
	case currencySymbols[code] != "":
		return sign + currencySymbols[code] + amount
	default:
		return sign + code + " " + amount
	}
}

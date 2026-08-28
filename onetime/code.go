package onetime

import (
	"crypto/rand"
	"fmt"
	"io"
)

// CodeLength is how many characters a code has.
//
// Six. It is what somebody expects to be asked for after an e-mail, it is short
// enough to hold in the head between two windows, and it is the length a form
// field marked as a numeric one-time code is built around.
const CodeLength = 6

// alphabet is the ten decimal digits.
//
// Digits and not letters, because every code here is read out of an e-mail and
// retyped by a person: there is no B and 8 to confuse, no case to get wrong, and
// on a phone the field becomes a keypad.
const alphabet = "0123456789"

// bias is the first byte value that has to be thrown away.
//
// Ten does not divide 256. Taking any byte modulo ten would make 0 through 5
// arrive twenty-six times in 256 and 6 through 9 twenty-five, which is a
// measurable bias in a code whose whole defence is that every value is as likely
// as the next. 250 is the largest multiple of ten that fits, so bytes 250 to 255
// are rejected and the twenty-five hundredths that remain are exactly uniform.
//
// The loop that does the rejecting is not a retry with a bound: it reads more
// randomness and keeps going, because a bounded retry that gives up has to
// decide what to return when it does, and there is no acceptable answer.
const bias = 250

// defaultRandom is where codes come from when nothing says otherwise.
var defaultRandom io.Reader = rand.Reader

// generate returns a fresh code.
//
// The entropy is a fact and not an aspiration: 10^6 equally likely codes, which
// is 19.93 bits. Uniform because of the rejection above; exhaustible in a
// million tries, which is why [Config.MaxAttempts] and not this number is what
// makes the code safe.
func (c *Codes) generate() (string, error) {
	code := make([]byte, 0, CodeLength)
	buf := make([]byte, CodeLength)

	for len(code) < CodeLength {
		if _, err := io.ReadFull(c.random, buf); err != nil {
			return "", fmt.Errorf("onetime: reading randomness for the code: %w", err)
		}
		for _, b := range buf {
			if b >= bias {
				continue
			}
			code = append(code, alphabet[int(b)%len(alphabet)])
			if len(code) == CodeLength {
				break
			}
		}
	}
	return string(code), nil
}

package qr

// Arithmetic in GF(2^8) modulo the primitive polynomial
// x^8 + x^4 + x^3 + x^2 + 1, which QR symbols use for Reed-Solomon error
// correction.
const primitivePolynomial = 0x11d

// gfExp maps an exponent to the field element 2^exp, and gfLog maps a non-zero
// field element back to its exponent. Both are built once at package
// initialisation from the primitive polynomial.
var (
	gfExp [512]byte
	gfLog [256]byte
)

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		gfExp[i] = byte(x)
		gfLog[x] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= primitivePolynomial
		}
	}
	// The upper half repeats the table so that a sum of two exponents can be
	// looked up without a modulo.
	for i := 255; i < 512; i++ {
		gfExp[i] = gfExp[i-255]
	}
}

// gfMul returns the product of a and b in GF(2^8).
func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

// generatorPolynomial returns the Reed-Solomon generator polynomial of the
// given degree, with coefficients in descending power order and a leading
// coefficient of one.
func generatorPolynomial(degree int) []byte {
	g := []byte{1}
	for i := 0; i < degree; i++ {
		// Multiply g by (x - 2^i), which in this field is (x + 2^i).
		next := make([]byte, len(g)+1)
		root := gfExp[i]
		for j, c := range g {
			next[j] ^= c
			next[j+1] ^= gfMul(c, root)
		}
		g = next
	}
	return g
}

// errorCodewords returns the count error correction codewords for the data
// block, computed as the remainder of the data polynomial divided by the
// generator polynomial of that degree.
func errorCodewords(data []byte, count int) []byte {
	g := generatorPolynomial(count)
	remainder := make([]byte, count)
	for _, d := range data {
		factor := d ^ remainder[0]
		copy(remainder, remainder[1:])
		remainder[count-1] = 0
		if factor != 0 {
			for i := 0; i < count; i++ {
				remainder[i] ^= gfMul(g[i+1], factor)
			}
		}
	}
	return remainder
}

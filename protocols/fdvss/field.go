package fdvss

import (
	"fmt"
	"math/big"
	"math/bits"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
)

type FieldParams struct {
	P uint64

	validated atomic.Bool
}

func validateFieldLayout(f *FieldParams) error {
	if f == nil {
		return fmt.Errorf("nil field params")
	}
	p := f.P
	if p < 2 {
		return fmt.Errorf("field modulus P must be >= 2")
	}
	m := p - 1
	hi, _ := bits.Mul64(m, m)
	if hi != 0 {
		return fmt.Errorf("field modulus P too large: require (P-1)^2 <= 2^64-1 for ModMul (got P=%d)", p)
	}
	return nil
}

func NewFieldParams(p uint64) (*FieldParams, error) {
	f := &FieldParams{P: p}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return f, nil
}

func MustNewFieldParams(p uint64) *FieldParams {
	f, err := NewFieldParams(p)
	if err != nil {
		panic("fdvss.MustNewFieldParams: " + err.Error())
	}
	return f
}

func (f *FieldParams) Validate() error {
	if f.validated.Load() {
		return validateFieldLayout(f)
	}
	if err := validateFieldLayout(f); err != nil {
		return err
	}
	p := f.P
	if !big.NewInt(0).SetUint64(p).ProbablyPrime(20) {
		return fmt.Errorf("field modulus P=%d is not prime (Miller-Rabin)", p)
	}
	f.validated.Store(true)
	return nil
}

func (f *FieldParams) Uint64FromInt(val int) uint64 {
	p := f.P
	if val < 0 {
		return p - uint64(-val)%p
	}
	return uint64(val) % p
}

func (f *FieldParams) Uint64FromUint64(val uint64) uint64 {
	return val % f.P
}

func (f *FieldParams) ModAdd(a, b uint64) uint64 {
	p := f.P
	aMod := a % p
	bMod := b % p
	result := aMod + bMod
	if result >= p {
		result -= p
	}
	return result
}

func (f *FieldParams) ModSub(a, b uint64) uint64 {
	p := f.P
	aMod := a % p
	bMod := b % p
	if aMod >= bMod {
		return aMod - bMod
	}
	return p + aMod - bMod
}

func (f *FieldParams) ModMul(a, b uint64) uint64 {
	p := f.P
	aMod := a % p
	bMod := b % p
	return (aMod * bMod) % p
}

func (f *FieldParams) ModAddCanon(a, b uint64) uint64 {
	p := f.P
	s := a + b
	if s >= p {
		s -= p
	}
	return s
}

func (f *FieldParams) ModSubCanon(a, b uint64) uint64 {
	p := f.P
	if a >= b {
		return a - b
	}
	return p + a - b
}

func (f *FieldParams) ModMulCanon(a, b uint64) uint64 {
	return (a * b) % f.P
}

func (f *FieldParams) ModPow(base, exp uint64) uint64 {
	baseMod := base % f.P
	if exp == 0 {
		return 1
	}
	if exp == 1 {
		return baseMod
	}
	result := uint64(1)
	for exp > 0 {
		if exp&1 != 0 {
			result = f.ModMul(result, baseMod)
		}
		baseMod = f.ModMul(baseMod, baseMod)
		exp >>= 1
	}
	return result
}

func (f *FieldParams) ModInv(a uint64) (uint64, error) {
	p := f.P
	aMod := a % p
	if aMod == 0 {
		return 0, fmt.Errorf("division by zero: cannot invert 0")
	}
	oldR := aMod
	r := p
	oldS := uint64(1)
	s := uint64(0)
	for r != 0 {
		quotient := oldR / r
		oldR, r = r, oldR-quotient*r
		oldS, s = s, f.ModSub(oldS, f.ModMul(quotient, s))
	}
	if oldR != 1 {
		return 0, fmt.Errorf("gcd is not 1, cannot compute inverse")
	}
	return oldS, nil
}

func (f *FieldParams) ModDiv(a, b uint64) (uint64, error) {
	bInv, err := f.ModInv(b)
	if err != nil {
		return 0, err
	}
	return f.ModMul(a, bInv), nil
}

func Uint64Zero() uint64 { return 0 }

func Uint64One() uint64 { return 1 }

func Uint64Equal(a, b uint64) bool { return a == b }

func Uint64IsZero(a uint64) bool { return a == 0 }

func ResolveBenchN(benchFlagN int) (int, error) {
	n := 7
	if benchFlagN > 0 {
		n = benchFlagN
	} else if envN := os.Getenv("N"); envN != "" {
		parsed, err := strconv.Atoi(envN)
		if err != nil {
			return 0, fmt.Errorf("environment variable N is %q: %w", envN, err)
		}
		n = parsed
	}
	if n < 4 {
		return 0, fmt.Errorf("N value %d is invalid: n must be >= 4", n)
	}
	return n, nil
}

func MustResolveBenchN(benchFlagN int) int {
	n, err := ResolveBenchN(benchFlagN)
	if err != nil {
		panic(err)
	}
	return n
}

func ResolveBenchD(n int, benchFlagDegree int) (int, error) {
	var d int
	have := false
	if benchFlagDegree >= 0 {
		d = benchFlagDegree
		have = true
	} else if s := strings.TrimSpace(os.Getenv("FDVSS_D")); s != "" {
		parsed, err := strconv.Atoi(s)
		if err != nil {
			return 0, fmt.Errorf("FDVSS_D is not an integer: %q: %w", s, err)
		}
		d = parsed
		have = true
	}
	if !have {
		return (n - 1) / 3, nil
	}
	if d < 0 {
		return 0, fmt.Errorf("degree D must be >= 0, got %d", d)
	}
	if n < 3*d+1 {
		return 0, fmt.Errorf("N=%d too small for degree D=%d: require N >= 3*D+1 (= %d)", n, d, 3*d+1)
	}
	return d, nil
}

func MustResolveBenchD(n int, benchFlagDegree int) int {
	d, err := ResolveBenchD(n, benchFlagDegree)
	if err != nil {
		panic(err)
	}
	return d
}

func ResolveBenchFieldP(p uint64) (*FieldParams, error) {
	if p == 0 {
		return nil, fmt.Errorf("prime P required: go test ... -args -field-p=<P>")
	}
	return NewFieldParams(p)
}

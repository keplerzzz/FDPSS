package fdvss

import (
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
)

type lagrangePoint struct {
	x uint64
	y uint64
}

func interpolateAtZero(f *FieldParams, points []lagrangePoint, degree int) (uint64, error) {
	if len(points) <= degree {
		return 0, fmt.Errorf("not enough points: have %d need %d", len(points), degree+1)
	}
	result := Uint64Zero()
	for i := range points {
		numerator := Uint64One()
		denominator := Uint64One()
		for j := range points {
			if i == j {
				continue
			}
			diff := f.ModSubCanon(points[j].x, points[i].x)
			if Uint64Equal(diff, Uint64Zero()) {
				return 0, fmt.Errorf("duplicate x in interpolation")
			}
			numerator = f.ModMulCanon(numerator, points[j].x)
			denominator = f.ModMulCanon(denominator, diff)
		}
		inv, err := f.ModInv(denominator)
		if err != nil {
			return 0, err
		}
		factor := f.ModMulCanon(numerator, inv)
		term := f.ModMulCanon(points[i].y, factor)
		result = f.ModAddCanon(result, term)
	}
	return result, nil
}

var newtonDenomInvCache sync.Map

func xsToNewtonCacheKey(f *FieldParams, xs []uint64) string {
	b := make([]byte, 8+8*len(xs))
	binary.LittleEndian.PutUint64(b[0:8], f.P)
	for i, x := range xs {
		binary.LittleEndian.PutUint64(b[8+i*8:], x%f.P)
	}
	return string(b)
}

func newtonDenomInvCount(degree int) int {
	return degree * (degree + 1) / 2
}

func computeNewtonDenomInvs(f *FieldParams, xs []uint64, degree int) ([]uint64, error) {
	nInv := newtonDenomInvCount(degree)
	if nInv == 0 {
		return nil, nil
	}
	out := make([]uint64, 0, nInv)
	for j := 1; j <= degree; j++ {
		for i := 0; i <= degree-j; i++ {
			den := f.ModSub(xs[i+j], xs[i])
			if Uint64IsZero(den) {
				return nil, fmt.Errorf("duplicate x in newton interpolation")
			}
			inv, err := f.ModInv(den)
			if err != nil {
				return nil, err
			}
			out = append(out, inv)
		}
	}
	return out, nil
}

func getOrComputeNewtonDenomInvs(f *FieldParams, xs []uint64, degree int) ([]uint64, error) {
	if degree == 0 {
		return nil, nil
	}
	key := xsToNewtonCacheKey(f, xs)
	if v, ok := newtonDenomInvCache.Load(key); ok {
		return v.([]uint64), nil
	}
	invs, err := computeNewtonDenomInvs(f, xs, degree)
	if err != nil {
		return nil, err
	}
	actual, _ := newtonDenomInvCache.LoadOrStore(key, invs)
	return actual.([]uint64), nil
}

func WarmNewtonInterpolationCacheForXs(f *FieldParams, xs []uint64, degree int) error {
	if f == nil {
		return fmt.Errorf("nil field params")
	}
	m := degree + 1
	if len(xs) != m {
		return fmt.Errorf("warm newton: need len(xs)=%d, have %d", m, len(xs))
	}
	norm := make([]uint64, m)
	p := f.P
	for i := range xs {
		norm[i] = xs[i] % p
	}
	key := xsToNewtonCacheKey(f, norm)
	if _, ok := newtonDenomInvCache.Load(key); ok {
		return nil
	}
	invs, err := computeNewtonDenomInvs(f, norm, degree)
	if err != nil {
		return err
	}
	newtonDenomInvCache.Store(key, invs)
	return nil
}

func polyMulXMinusInto(f *FieldParams, dst []uint64, p []uint64, xk uint64) []uint64 {
	mod := f.P
	xk = xk % mod
	L := len(p)
	dst[0] = f.ModSub(0, f.ModMul(xk, p[0]%mod))
	for i := 1; i < L; i++ {
		dst[i] = f.ModSub(p[i-1]%mod, f.ModMul(xk, p[i]%mod))
	}
	dst[L] = p[L-1] % mod
	return dst[:L+1]
}

func newtonInterpolateMonomialCoeffs(f *FieldParams, points []lagrangePoint, degree int) ([]uint64, error) {
	mod := f.P
	m := len(points)
	if m != degree+1 {
		return nil, fmt.Errorf("newton: need %d points, have %d", degree+1, m)
	}
	xs := make([]uint64, m)
	col := make([]uint64, m)
	for i := range points {
		xs[i] = points[i].x % mod
		col[i] = points[i].y % mod
	}
	dd := make([]uint64, m)
	dd[0] = col[0]
	invs, err := getOrComputeNewtonDenomInvs(f, xs, degree)
	if err != nil {
		return nil, err
	}
	if len(invs) != newtonDenomInvCount(degree) {
		return nil, fmt.Errorf("newton: internal inv count mismatch")
	}
	idx := 0
	for j := 1; j <= degree; j++ {
		for i := 0; i <= degree-j; i++ {
			col[i] = f.ModMul(f.ModSub(col[i+1], col[i]), invs[idx])
			idx++
		}
		dd[j] = col[0]
	}
	a := make([]uint64, m+1)
	b := make([]uint64, m+1)
	p := a[:1]
	p[0] = dd[degree] % mod
	useB := true
	for j := degree - 1; j >= 0; j-- {
		if useB {
			p = polyMulXMinusInto(f, b, p, xs[j])
		} else {
			p = polyMulXMinusInto(f, a, p, xs[j])
		}
		p[0] = f.ModAdd(p[0], dd[j])
		useB = !useB
	}
	out := make([]uint64, len(p))
	copy(out, p)
	return polyTrim(out), nil
}

func tryReedSolomonDecodeFastPath(f *FieldParams, sorted []lagrangePoint, degree int) (uint64, bool, error) {
	sub := sorted[:degree+1]
	coeffs, err := newtonInterpolateMonomialCoeffs(f, sub, degree)
	if err != nil {
		return 0, false, nil
	}
	q := polyTrim(coeffs)
	if len(q) == 0 {
		return 0, false, nil
	}
	if polyDegree(q) > degree {
		return 0, false, nil
	}
	mod := f.P
	for _, pt := range sorted[degree+1:] {
		x := pt.x % mod
		if !Uint64Equal(polyEvalAt(f, q, x), pt.y%mod) {
			return 0, false, nil
		}
	}
	return q[0] % mod, true, nil
}

func reedSolomonDecodeBerlekampWelch(f *FieldParams, points []lagrangePoint, degree, e int) (uint64, error) {
	mod := f.P
	n := len(points)
	numN := degree + e + 1
	numE := e
	numVar := numN + numE
	if numVar == 0 {
		return 0, fmt.Errorf("invalid decode parameters")
	}
	aug := make([][]uint64, n)
	for i := range aug {
		aug[i] = make([]uint64, numVar+1)
	}
	for i, pt := range points {
		x := pt.x % mod
		y := pt.y % mod
		pow := uint64(1)
		for j := 0; j < numN; j++ {
			aug[i][j] = pow
			pow = f.ModMul(pow, x)
		}
		yPow := Uint64One()
		for k := 0; k < numE; k++ {
			aug[i][numN+k] = f.ModSub(0, f.ModMul(y, yPow))
			yPow = f.ModMul(yPow, x)
		}
		aug[i][numVar] = f.ModMul(y, yPow)
	}
	sol, err := gaussianSolveFP(f, aug)
	if err != nil {
		return 0, fmt.Errorf("berlekamp-welch: %w", err)
	}
	nCoeffs := sol[:numN]
	eCoeffs := make([]uint64, e+1)
	for k := 0; k < numE; k++ {
		eCoeffs[k] = sol[numN+k]
	}
	eCoeffs[e] = Uint64One()
	q, rem, err := polyDivMod(f, nCoeffs, eCoeffs)
	if err != nil {
		return 0, fmt.Errorf("berlekamp-welch: %w", err)
	}
	if !polyIsZero(f, rem) {
		return 0, fmt.Errorf("berlekamp-welch: remainder after division")
	}
	if polyDegree(q) > degree {
		return 0, fmt.Errorf("berlekamp-welch: decoded degree too large")
	}
	disagree := 0
	for _, pt := range points {
		if !Uint64Equal(polyEvalAt(f, q, pt.x), pt.y) {
			disagree++
		}
	}
	if disagree > e {
		return 0, fmt.Errorf("berlekamp-welch: too many disagreements (%d > %d)", disagree, e)
	}
	return polyEvalAt(f, q, Uint64Zero()), nil
}

func reedSolomonDecode(f *FieldParams, points []lagrangePoint, degree int) (uint64, error) {
	return reedSolomonDecodeImpl(f, points, degree, false)
}

func reedSolomonDecodeSortedStrict(f *FieldParams, points []lagrangePoint, degree int) (uint64, error) {
	return reedSolomonDecodeImpl(f, points, degree, true)
}

func reedSolomonDecodeImpl(f *FieldParams, points []lagrangePoint, degree int, xsStrictIncreasing bool) (uint64, error) {
	n := len(points)
	if n <= degree {
		return 0, fmt.Errorf("insufficient points: have %d need %d", n, degree+1)
	}
	if xsStrictIncreasing {
		for i := 1; i < n; i++ {
			if points[i].x <= points[i-1].x {
				return 0, fmt.Errorf("duplicate or unsorted x in sorted RS decode input at index %d", i)
			}
		}
	} else {
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				if Uint64Equal(points[i].x, points[j].x) {
					return 0, fmt.Errorf("duplicate x in decode input")
				}
			}
		}
	}
	e := (n - degree - 1) / 2
	if e < 0 {
		e = 0
	}
	if n == degree+1 {
		return interpolateAtZero(f, points, degree)
	}

	var pts []lagrangePoint
	if xsStrictIncreasing {
		pts = points
	} else {
		tmp := make([]lagrangePoint, n)
		copy(tmp, points)
		sort.Slice(tmp, func(i, j int) bool { return tmp[i].x < tmp[j].x })
		pts = tmp
	}

	if v, ok, err := tryReedSolomonDecodeFastPath(f, pts, degree); err != nil {
		return 0, err
	} else if ok {
		return v, nil
	}
	return reedSolomonDecodeBerlekampWelch(f, pts, degree, e)
}

func gaussianSolveFP(f *FieldParams, aug [][]uint64) ([]uint64, error) {
	mod := f.P
	m := len(aug)
	if m == 0 {
		return nil, fmt.Errorf("empty system")
	}
	n := len(aug[0]) - 1
	for i := range aug {
		if len(aug[i]) != n+1 {
			return nil, fmt.Errorf("ragged augmented matrix")
		}
	}
	row := 0
	pivotCol := make([]int, m)
	for col := 0; col < n && row < m; col++ {
		piv := -1
		for r := row; r < m; r++ {
			if !Uint64IsZero(aug[r][col] % mod) {
				piv = r
				break
			}
		}
		if piv == -1 {
			continue
		}
		aug[row], aug[piv] = aug[piv], aug[row]
		pivotCol[row] = col
		inv, err := f.ModInv(aug[row][col] % mod)
		if err != nil {
			return nil, err
		}
		for c := col; c <= n; c++ {
			aug[row][c] = f.ModMul(aug[row][c], inv)
		}
		for r := row + 1; r < m; r++ {
			ff := aug[r][col] % mod
			if Uint64IsZero(ff) {
				continue
			}
			for c := col; c <= n; c++ {
				aug[r][c] = f.ModSub(aug[r][c], f.ModMul(ff, aug[row][c]))
			}
		}
		row++
	}
	for pr := row - 1; pr >= 0; pr-- {
		pc := pivotCol[pr]
		for r := pr - 1; r >= 0; r-- {
			ff := aug[r][pc] % mod
			if Uint64IsZero(ff) {
				continue
			}
			for c := pc; c <= n; c++ {
				aug[r][c] = f.ModSub(aug[r][c], f.ModMul(ff, aug[pr][c]))
			}
		}
	}
	for r := 0; r < m; r++ {
		allZero := true
		for c := 0; c < n; c++ {
			if !Uint64IsZero(aug[r][c] % mod) {
				allZero = false
				break
			}
		}
		if allZero && !Uint64IsZero(aug[r][n]%mod) {
			return nil, fmt.Errorf("linear system inconsistent")
		}
	}
	x := make([]uint64, n)
	for r := 0; r < row; r++ {
		c := pivotCol[r]
		x[c] = aug[r][n] % mod
	}
	return x, nil
}

func polyTrim(a []uint64) []uint64 {
	d := len(a) - 1
	for d > 0 && Uint64IsZero(a[d]) {
		d--
	}
	return a[:d+1]
}

func polyDegree(a []uint64) int {
	t := polyTrim(a)
	if len(t) == 1 && Uint64IsZero(t[0]) {
		return -1
	}
	return len(t) - 1
}

func polyIsZero(f *FieldParams, a []uint64) bool {
	mod := f.P
	for _, v := range a {
		if !Uint64IsZero(v % mod) {
			return false
		}
	}
	return true
}

func polyEvalAt(f *FieldParams, a []uint64, x uint64) uint64 {
	x = x % f.P
	var acc uint64
	pow := Uint64One()
	for _, c := range a {
		acc = f.ModAdd(acc, f.ModMul(c%f.P, pow))
		pow = f.ModMul(pow, x)
	}
	return acc
}

func polyDivMod(f *FieldParams, a, b []uint64) (q, r []uint64, err error) {
	mod := f.P
	a = polyTrim(a)
	b = polyTrim(b)
	if len(b) == 0 || (len(b) == 1 && Uint64IsZero(b[0])) {
		return nil, nil, fmt.Errorf("polyDivMod: zero divisor")
	}
	if polyDegree(a) < polyDegree(b) {
		return []uint64{0}, append([]uint64(nil), a...), nil
	}
	db := polyDegree(b)
	lcInv, err := f.ModInv(b[db] % mod)
	if err != nil {
		return nil, nil, err
	}
	rem := append([]uint64(nil), a...)
	dq := polyDegree(a) - db
	q = make([]uint64, dq+1)
	for !polyIsZero(f, rem) && polyDegree(rem) >= db {
		dr := polyDegree(rem)
		coef := f.ModMul(rem[dr], lcInv)
		shift := dr - db
		q[shift] = f.ModAdd(q[shift], coef)
		for i := 0; i <= db; i++ {
			rem[i+shift] = f.ModSub(rem[i+shift], f.ModMul(coef, b[i]))
		}
	}
	return polyTrim(q), polyTrim(rem), nil
}

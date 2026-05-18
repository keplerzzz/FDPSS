package fdvss

func specializeColumnCoefficients(f *FieldParams, coeffs [][]uint64, zVal uint64) []uint64 {
	if len(coeffs) == 0 {
		return nil
	}
	if len(coeffs[0]) == 0 {
		return nil
	}
	result := make([]uint64, len(coeffs))
	zPows := precomputePows(f, zVal, len(coeffs[0]))
	for xExp := 0; xExp < len(coeffs); xExp++ {
		sum := Uint64Zero()
		for zExp := 0; zExp < len(coeffs[xExp]); zExp++ {
			coeff := coeffs[xExp][zExp]
			if Uint64IsZero(coeff) {
				continue
			}
			term := f.ModMulCanon(coeff, zPows[zExp])
			sum = f.ModAddCanon(sum, term)
		}
		result[xExp] = sum
	}
	return result
}

func precomputePows(f *FieldParams, base uint64, length int) []uint64 {
	pows := make([]uint64, length)
	if length == 0 {
		return pows
	}
	pows[0] = Uint64One()
	baseMod := f.Uint64FromUint64(base)
	for i := 1; i < length; i++ {
		pows[i] = f.ModMulCanon(pows[i-1], baseMod)
	}
	return pows
}

func specializeCoefficients(f *FieldParams, coeffs [][]uint64, scalar uint64) []uint64 {
	if len(coeffs) == 0 {
		return nil
	}
	zSize := len(coeffs[0])
	result := make([]uint64, zSize)
	pows := precomputePows(f, scalar, len(coeffs))
	for exp := 0; exp < len(coeffs); exp++ {
		for zExp := 0; zExp < zSize; zExp++ {
			coeff := coeffs[exp][zExp]
			if Uint64IsZero(coeff) {
				continue
			}
			term := f.ModMulCanon(coeff, pows[exp])
			result[zExp] = f.ModAddCanon(result[zExp], term)
		}
	}
	return result
}

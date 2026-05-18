package fdvss

import (
	"fmt"
)

type TriPoly struct {
	Degree int
	Coeffs [][][]uint64
}

func NewTriPoly(degree int) *TriPoly {
	size := degree + 1
	coeffs := make([][][]uint64, size)
	for a := 0; a < size; a++ {
		coeffs[a] = make([][]uint64, size)
		for b := 0; b < size; b++ {
			coeffs[a][b] = make([]uint64, size)
			for c := 0; c < size; c++ {
				coeffs[a][b][c] = Uint64Zero()
			}
		}
	}
	return &TriPoly{Degree: degree, Coeffs: coeffs}
}

func (q *TriPoly) Randomize(f *FieldParams, secret uint64) error {
	if f == nil {
		return fmt.Errorf("nil field params")
	}
	if q.Coeffs == nil {
		size := q.Degree + 1
		q.Coeffs = make([][][]uint64, size)
		for a := 0; a < size; a++ {
			q.Coeffs[a] = make([][]uint64, size)
			for b := 0; b < size; b++ {
				q.Coeffs[a][b] = make([]uint64, size)
				for c := 0; c < size; c++ {
					q.Coeffs[a][b][c] = Uint64Zero()
				}
			}
		}

	}
	q.Coeffs[0][0][0] = secret % f.P

	totalCoeffs := (q.Degree+1)*(q.Degree+1)*(q.Degree+1) - 1
	randomCoeffs, err := sampleRandomModValuesBatch(f, totalCoeffs)
	if err != nil {
		return fmt.Errorf("sample random coefficients failed: %w", err)
	}
	randomOffset := 0

	for a := 0; a <= q.Degree; a++ {
		for b := 0; b <= q.Degree; b++ {
			for c := 0; c <= q.Degree; c++ {
				if a == 0 && b == 0 && c == 0 {
					continue
				}
				q.Coeffs[a][b][c] = randomCoeffs[randomOffset]
				randomOffset++

			}
		}
	}

	return nil
}

func (q *TriPoly) ProjectFixX(f *FieldParams, xVal uint64) [][]uint64 {
	size := q.Degree + 1
	xPows := make([]uint64, size)
	xPows[0] = Uint64One()
	xMod := f.Uint64FromUint64(xVal)
	for i := 1; i < size; i++ {
		xPows[i] = f.ModMulCanon(xPows[i-1], xMod)
	}
	return q.projectFixXWithPows(f, xPows)
}

func (q *TriPoly) projectFixXWithPows(f *FieldParams, xPows []uint64) [][]uint64 {
	size := q.Degree + 1
	if len(xPows) < size {
		return nil
	}
	matrix := make([][]uint64, size)
	for y := 0; y < size; y++ {
		matrix[y] = make([]uint64, size)
	}
	for a := 0; a < size; a++ {
		for b := 0; b < size; b++ {
			for c := 0; c < size; c++ {
				coeff := q.Coeffs[a][b][c]
				if Uint64IsZero(coeff) {
					continue
				}
				termCoeff := f.ModMulCanon(coeff, xPows[a])
				if Uint64IsZero(termCoeff) {
					continue
				}
				matrix[b][c] = f.ModAddCanon(matrix[b][c], termCoeff)
			}
		}
	}
	return matrix
}

func (q *TriPoly) ProjectFixY(f *FieldParams, yVal uint64) [][]uint64 {
	size := q.Degree + 1
	yPows := make([]uint64, size)
	yPows[0] = Uint64One()
	yMod := f.Uint64FromUint64(yVal)
	for i := 1; i < size; i++ {
		yPows[i] = f.ModMulCanon(yPows[i-1], yMod)
	}
	return q.projectFixYWithPows(f, yPows)
}

func (q *TriPoly) projectFixYWithPows(f *FieldParams, yPows []uint64) [][]uint64 {
	size := q.Degree + 1
	if len(yPows) < size {
		return nil
	}
	matrix := make([][]uint64, size)
	for x := 0; x < size; x++ {
		matrix[x] = make([]uint64, size)
	}
	for a := 0; a < size; a++ {
		for b := 0; b < size; b++ {
			for c := 0; c < size; c++ {
				coeff := q.Coeffs[a][b][c]
				if Uint64IsZero(coeff) {
					continue
				}
				termCoeff := f.ModMulCanon(coeff, yPows[b])
				if Uint64IsZero(termCoeff) {
					continue
				}
				matrix[a][c] = f.ModAddCanon(matrix[a][c], termCoeff)
			}
		}
	}
	return matrix
}

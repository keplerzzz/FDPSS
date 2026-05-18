package random_generation

import (
	"fmt"

	"go-fdvss-fdpss/protocols/fdvss"
)

func BuildVandermondeMatrix(f *fdvss.FieldParams, n, t int, betas []uint64) ([][]uint64, error) {
	if f == nil {
		return nil, fmt.Errorf("nil field params")
	}
	if len(betas) != n {
		return nil, fmt.Errorf("beta values count %d != n %d", len(betas), n)
	}
	if n <= t {
		return nil, fmt.Errorf("n (%d) must be greater than t (%d)", n, t)
	}

	cols := n - t
	matrix := make([][]uint64, n)

	for i := 0; i < n; i++ {
		matrix[i] = make([]uint64, cols)
		beta := f.Uint64FromUint64(betas[i])

		power := fdvss.Uint64One()
		for j := 0; j < cols; j++ {
			matrix[i][j] = power
			if j < cols-1 {
				power = f.ModMul(power, beta)
			}
		}
	}

	return matrix, nil
}

func BuildVandermondeMatrixFromIndices(f *fdvss.FieldParams, n, t int) ([][]uint64, error) {
	betas := make([]uint64, n)
	for i := 0; i < n; i++ {
		betas[i] = f.Uint64FromInt(i + 1)
	}
	return BuildVandermondeMatrix(f, n, t, betas)
}

func MatrixTranspose(matrix [][]uint64) ([][]uint64, error) {
	if len(matrix) == 0 {
		return nil, fmt.Errorf("empty matrix")
	}
	rows := len(matrix)
	cols := len(matrix[0])

	for i := 1; i < rows; i++ {
		if len(matrix[i]) != cols {
			return nil, fmt.Errorf("inconsistent row lengths")
		}
	}

	transposed := make([][]uint64, cols)
	for j := 0; j < cols; j++ {
		transposed[j] = make([]uint64, rows)
		for i := 0; i < rows; i++ {
			transposed[j][i] = matrix[i][j]
		}
	}

	return transposed, nil
}

func MatrixVectorMultiply(f *fdvss.FieldParams, matrix [][]uint64, vector []uint64) ([]uint64, error) {
	if f == nil {
		return nil, fmt.Errorf("nil field params")
	}
	if len(matrix) == 0 {
		return nil, fmt.Errorf("empty matrix")
	}
	rows := len(matrix)
	cols := len(matrix[0])

	if len(vector) != cols {
		return nil, fmt.Errorf("vector length %d != matrix columns %d", len(vector), cols)
	}

	result := make([]uint64, rows)
	for i := 0; i < rows; i++ {
		if len(matrix[i]) != cols {
			return nil, fmt.Errorf("inconsistent row length at row %d", i)
		}
		sum := fdvss.Uint64Zero()
		for j := 0; j < cols; j++ {
			term := f.ModMul(matrix[i][j], vector[j])
			sum = f.ModAdd(sum, term)
		}
		result[i] = sum
	}

	return result, nil
}

package fdvss

import "fmt"

type ShamirWitnessPoint struct {
	X uint64
	Y uint64
}

func ReconstructPolynomialValueAtZero(f *FieldParams, points []ShamirWitnessPoint, degree int) (uint64, error) {
	if f == nil {
		return 0, fmt.Errorf("nil field params")
	}
	if err := f.Validate(); err != nil {
		return 0, err
	}
	if len(points) == 0 {
		return 0, fmt.Errorf("no points")
	}
	if len(points) <= degree {
		return 0, fmt.Errorf("insufficient points: have %d need %d", len(points), degree+1)
	}
	p := f.P
	pts := make([]lagrangePoint, len(points))
	for i := range points {
		pts[i] = lagrangePoint{x: points[i].X % p, y: points[i].Y % p}
	}
	return reedSolomonDecode(f, pts, degree)
}

package fdvss

import (
	"fmt"

	"go-fdvss-fdpss/communication"
	"go-fdvss-fdpss/primitives"
)

type Committees struct {
	Dealers []int
	Com1    []int
	Com2    []int
	Com3    []int
	Com4    []int
}

type Indices struct {
	Dealers int
	Com1    int
	Com2    int
	Com3    int
	Com4    int
}

func (c *Committees) Indices(id int) Indices {
	return Indices{
		Dealers: primitives.IntIndexOf(c.Dealers, id),
		Com1:    primitives.IntIndexOf(c.Com1, id),
		Com2:    primitives.IntIndexOf(c.Com2, id),
		Com3:    primitives.IntIndexOf(c.Com3, id),
		Com4:    primitives.IntIndexOf(c.Com4, id),
	}
}

type PublicInput struct {
	VSSParams  primitives.Params
	Committees Committees

	Field *FieldParams
}

type PrivateInput struct {
	ID     int
	Secret uint64
	BC     communication.BroadcastChannel
	P2P    communication.PointToPointChannel
}

func (pub *PublicInput) Validate() error {
	if pub == nil {
		return fmt.Errorf("nil public input")
	}
	if pub.Field == nil {
		return fmt.Errorf(
			"PublicInput.Field is nil: assign a prime field with NewFieldParams(P) (e.g. f, err := NewFieldParams(P); pub.Field = f). " +
				"Require prime P, (P-1)^2 fits uint64 modular multiply, and N < P for Shamir points 1..N; env vars do not set the modulus",
		)
	}
	if err := pub.Field.Validate(); err != nil {
		return fmt.Errorf("field params: %w", err)
	}
	n := pub.VSSParams.N
	if n <= 0 {
		return fmt.Errorf("invalid VSS parameter N")
	}
	if pub.VSSParams.D < 0 {
		return fmt.Errorf("invalid polynomial degree")
	}
	if n < 3*pub.VSSParams.D+1 {
		return fmt.Errorf("committee size n=%d must satisfy n >= 3*t+1 (t=%d)", n, pub.VSSParams.D)
	}
	p := pub.Field.P
	if uint64(n) >= p {
		return fmt.Errorf("committee size n=%d must satisfy n < field prime P=%d (Shamir points 1..n must be distinct and nonzero in F_p)", n, p)
	}

	lengths := []struct {
		name string
		size int
	}{
		{"Com1", len(pub.Committees.Com1)},
		{"Com2", len(pub.Committees.Com2)},
		{"Com3", len(pub.Committees.Com3)},
		{"Com4", len(pub.Committees.Com4)},
	}
	for _, entry := range lengths {
		if entry.size != n {
			return fmt.Errorf("committee %s must have exactly n=%d members (got %d)", entry.name, n, entry.size)
		}
	}
	if len(pub.Committees.Dealers) == 0 {
		return fmt.Errorf("at least one dealer is required")
	}
	return nil
}

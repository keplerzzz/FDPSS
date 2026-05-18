package fdvss

import (
	"fmt"
)

type FutureColumn struct {
	DealerID int
	Com1Seq  int
	Matrix   [][]uint64
}

type FutureCrossColumn struct {
	DealerID int
	Com1Seq  int
	PeerCom1 int
	Coeffs   []uint64
}

func ReconstructFutureColumns(
	pub *PublicInput,
	com3Msgs []Com3Message,
	unhappyByDealer map[int]map[int]struct{},
) ([]FutureColumn, []FutureCrossColumn, error) {
	if pub == nil {
		return nil, nil, fmt.Errorf("nil public input")
	}
	if err := pub.Validate(); err != nil {
		return nil, nil, err
	}
	f := pub.Field
	size := pub.VSSParams.D + 1
	if size <= 0 {
		return nil, nil, fmt.Errorf("invalid degree bound")
	}

	type dealerColumnKey struct{ dealerID, com1Seq int }
	dealerBuckets := make(map[dealerColumnKey]map[int][][]uint64)

	type crossColumnKey struct{ dealerID, com1Seq, peerCom1 int }
	crossBuckets := make(map[crossColumnKey]map[int][]uint64)

	for _, msg := range com3Msgs {
		for _, item := range msg.BroadcastData {
			unhappySet := unhappyByDealer[item.DealerID]

			for _, share := range item.DealerShares {

				if _, ownerUnhappy := unhappySet[share.Com1Seq]; !ownerUnhappy {
					continue
				}
				key := dealerColumnKey{dealerID: share.DealerID, com1Seq: share.Com1Seq}
				bucket := dealerBuckets[key]
				if bucket == nil {
					bucket = make(map[int][][]uint64, len(pub.Committees.Com3))
					dealerBuckets[key] = bucket
				}
				bucket[share.Com3Seq] = share.Matrix
			}

			for _, share := range item.Com1PeerShares {

				if _, ownerUnhappy := unhappySet[share.Com1Seq]; !ownerUnhappy {
					continue
				}
				if _, peerUnhappy := unhappySet[share.PeerIndex]; peerUnhappy {
					continue
				}

				key := crossColumnKey{dealerID: share.DealerID, com1Seq: share.Com1Seq, peerCom1: share.PeerIndex}
				bucket := crossBuckets[key]
				if bucket == nil {
					bucket = make(map[int][]uint64, len(pub.Committees.Com3))
					crossBuckets[key] = bucket
				}

				if len(share.Matrix) != size {
					return nil, nil, fmt.Errorf("invalid cross share matrix size from com3 seq %d: expected %d, got %d", share.Com3Seq, size, len(share.Matrix))
				}
				bucket[share.Com3Seq] = share.Matrix
			}
		}
	}

	columns := make([]FutureColumn, 0, len(dealerBuckets))
	for key, shareMap := range dealerBuckets {
		matrix, err := interpolateFutureMatrix(f, size, shareMap)
		if err != nil {
			return nil, nil, fmt.Errorf("reconstruct dealer column dealer=%d com1Seq=%d: %w", key.dealerID, key.com1Seq, err)
		}
		columns = append(columns, FutureColumn{
			DealerID: key.dealerID,
			Com1Seq:  key.com1Seq,
			Matrix:   matrix,
		})
	}

	crossColumns := make([]FutureCrossColumn, 0, len(crossBuckets))
	for key, shareMap := range crossBuckets {
		coeffs, err := interpolateCrossPolynomial(f, size, shareMap)
		if err != nil {
			return nil, nil, fmt.Errorf("reconstruct cross column dealer=%d com1Seq=%d peer=%d: %w", key.dealerID, key.com1Seq, key.peerCom1, err)
		}
		crossColumns = append(crossColumns, FutureCrossColumn{
			DealerID: key.dealerID,
			Com1Seq:  key.com1Seq,
			PeerCom1: key.peerCom1,
			Coeffs:   coeffs,
		})
	}

	return columns, crossColumns, nil
}

func interpolateFutureMatrix(f *FieldParams, size int, shares map[int][][]uint64) ([][]uint64, error) {
	degree := size - 1
	matrix := make([][]uint64, size)
	for x := 0; x < size; x++ {
		matrix[x] = make([]uint64, size)
		for z := 0; z < size; z++ {
			points := make([]lagrangePoint, 0, len(shares))
			for com3Seq, mat := range shares {
				if x >= len(mat) || z >= len(mat[x]) {
					return nil, fmt.Errorf("share matrix from com3 seq %d has incompatible size", com3Seq)
				}
				xScalar := uint64(com3Seq)
				points = append(points, lagrangePoint{
					x: xScalar,
					y: mat[x][z],
				})
			}
			if len(points) <= degree {
				return nil, fmt.Errorf("not enough shares for coefficient (%d,%d)", x, z)
			}
			value, err := reedSolomonDecode(f, points, degree)
			if err != nil {
				return nil, err
			}
			matrix[x][z] = value
		}
	}
	return matrix, nil
}

func interpolateCrossPolynomial(f *FieldParams, size int, shares map[int][]uint64) ([]uint64, error) {
	degree := size - 1
	coeffs := make([]uint64, size)
	for z := 0; z < size; z++ {
		points := make([]lagrangePoint, 0, len(shares))
		for com3Seq, coeffsShare := range shares {
			if z >= len(coeffsShare) {
				return nil, fmt.Errorf("share polynomial from com3 seq %d has incompatible size", com3Seq)
			}
			xScalar := uint64(com3Seq)
			points = append(points, lagrangePoint{
				x: xScalar,
				y: coeffsShare[z],
			})
		}
		if len(points) <= degree {
			return nil, fmt.Errorf("not enough shares for coefficient w=%d", z)
		}
		value, err := reedSolomonDecode(f, points, degree)
		if err != nil {
			return nil, err
		}
		coeffs[z] = value
	}
	return coeffs, nil
}

func evaluatePolynomial(f *FieldParams, coeffs []uint64, x uint64) uint64 {
	res := Uint64Zero()
	xMod := x % f.P
	for i := len(coeffs) - 1; i >= 0; i-- {
		res = f.ModAdd(f.ModMul(res, xMod), coeffs[i])
	}
	return res
}

package fdvss

import (
	"fmt"
)

func PerformCom1ToCom2(pub *PublicInput, prv *PrivateInput, entry Com1Entry) ([]Com1ToCom2Message, error) {
	if err := pub.Validate(); err != nil {
		return nil, err
	}
	f := pub.Field
	indices := pub.Committees.Indices(prv.ID)
	if indices.Com1 < 0 {
		return nil, fmt.Errorf("party %d is not in Com1", prv.ID)
	}

	matEval, err := newShamirBatchEvaluator(f, &pub.VSSParams)
	if err != nil {
		return nil, err
	}
	rowShares, err := shareFutureMatrixWithEvaluator(matEval, &pub.VSSParams, entry.Row.Matrix)
	if err != nil {
		return nil, fmt.Errorf("com1 %d failed to share row projection for dealer %d: %w", prv.ID, entry.DealerID, err)
	}
	colShares, err := shareFutureMatrixWithEvaluator(matEval, &pub.VSSParams, entry.Col.Matrix)
	if err != nil {
		return nil, fmt.Errorf("com1 %d failed to share col projection for dealer %d: %w", prv.ID, entry.DealerID, err)
	}

	msgs := make([]Com1ToCom2Message, 0, len(pub.Committees.Com2))
	for idx, com2ID := range pub.Committees.Com2 {
		msgs = append(msgs, Com1ToCom2Message{
			DealerID: entry.DealerID,
			Com1ID:   entry.Com1ID,
			Origin:   prv.ID,
			Target:   com2ID,
			RowShare: rowShares[idx],
			ColShare: colShares[idx],
		})
	}

	return msgs, nil
}

func PerformCom1ToCom3(pub *PublicInput, prv *PrivateInput, entry Com1Entry) ([]Com1ToCom3Message, error) {
	if err := pub.Validate(); err != nil {
		return nil, err
	}
	f := pub.Field
	indices := pub.Committees.Indices(prv.ID)
	if indices.Com1 < 0 {
		return nil, fmt.Errorf("party %d is not in Com1", prv.ID)
	}
	com1Pos := make(map[int]int, len(pub.Committees.Com1))
	for idx, id := range pub.Committees.Com1 {
		com1Pos[id] = idx
	}
	com1Seq := entry.Com1ID
	if com1Seq <= 0 || com1Seq > len(pub.Committees.Com1) {
		return nil, fmt.Errorf("invalid Com1 sequence number %d (must be between 1 and %d)", com1Seq, len(pub.Committees.Com1))
	}
	com3IDs := pub.Committees.Com3
	msgs := make([]Com1ToCom3Message, 0, len(pub.Committees.Com1)*len(com3IDs))
	crossByPeerSeq := precomputeCrossCoeffsByPeerSeq(f, entry.Row.Matrix, len(pub.Committees.Com1))
	polyEval, err := newShamirBatchEvaluator(f, &pub.VSSParams)
	if err != nil {
		return nil, err
	}

	for _, peerID := range pub.Committees.Com1 {
		peerIdx, ok := com1Pos[peerID]
		if !ok {
			return nil, fmt.Errorf("peer %d not in Com1", peerID)
		}
		crossCoeffs := crossByPeerSeq[peerIdx]
		if len(crossCoeffs) != pub.VSSParams.D+1 {
			continue
		}
		shares, err := shareSinglePolyWithEvaluator(polyEval, crossCoeffs)
		if err != nil {
			return nil, fmt.Errorf("com1 %d failed to share F(i,i',Z) for (seq=%d,peer=%d): %w",
				prv.ID, entry.Com1ID, peerID, err)
		}
		for idx := range com3IDs {
			matrix := make([]uint64, len(shares[idx]))
			copy(matrix, shares[idx])
			msgs = append(msgs, Com1ToCom3Message{
				Share: Com1ColumnShare{
					DealerID:  entry.DealerID,
					Com1Seq:   com1Seq,
					PeerIndex: peerIdx + 1,
					Com3Seq:   idx + 1,
					Matrix:    matrix,
				},
			})
		}
	}
	return msgs, nil
}

func precomputeCrossCoeffsByPeerSeq(f *FieldParams, rowMatrix [][]uint64, peerCount int) [][]uint64 {
	if peerCount <= 0 || len(rowMatrix) == 0 || len(rowMatrix[0]) == 0 {
		return nil
	}
	yDegree := len(rowMatrix)
	zDegree := len(rowMatrix[0])
	peerPows := make([][]uint64, peerCount)
	for peerSeq := 1; peerSeq <= peerCount; peerSeq++ {
		pows := make([]uint64, yDegree)
		pows[0] = Uint64One()
		scalar := uint64(peerSeq)
		for exp := 1; exp < yDegree; exp++ {
			pows[exp] = f.ModMulCanon(pows[exp-1], scalar)
		}
		peerPows[peerSeq-1] = pows
	}
	out := make([][]uint64, peerCount)
	for i := range out {
		out[i] = make([]uint64, zDegree)
	}
	for yExp := 0; yExp < yDegree; yExp++ {
		for zExp := 0; zExp < zDegree; zExp++ {
			coeff := rowMatrix[yExp][zExp]
			if Uint64IsZero(coeff) {
				continue
			}
			for peer := 0; peer < peerCount; peer++ {
				term := f.ModMulCanon(coeff, peerPows[peer][yExp])
				out[peer][zExp] = f.ModAddCanon(out[peer][zExp], term)
			}
		}
	}
	return out
}

func PerformCom1ToCom4(pub *PublicInput, prv *PrivateInput, entry Com1Entry) ([]Com1ToCom4Message, error) {
	if err := pub.Validate(); err != nil {
		return nil, err
	}
	f := pub.Field
	indices := pub.Committees.Indices(prv.ID)
	if indices.Com1 < 0 {
		return nil, fmt.Errorf("party %d is not in Com1", prv.ID)
	}
	if len(pub.Committees.Com4) == 0 {
		return nil, fmt.Errorf("no recipients configured")
	}
	if len(entry.Col.Matrix) == 0 {
		return nil, fmt.Errorf("com1 seq=%d missing column coefficients for dealer %d", entry.Com1ID, entry.DealerID)
	}
	msgs := make([]Com1ToCom4Message, 0, len(pub.Committees.Com4))
	for idx := range pub.Committees.Com4 {
		k := uint64(idx + 1)
		column := specializeColumnCoefficients(f, entry.Col.Matrix, k)
		if len(column) == 0 {
			return nil, fmt.Errorf("failed to evaluate column for recipient seq %d", idx+1)
		}
		msgs = append(msgs, Com1ToCom4Message{
			DealerID: entry.DealerID,
			Com1ID:   entry.Com1ID,
			Target:   idx + 1,
			Coeffs:   column,
		})
	}
	return msgs, nil
}

package fdvss

import (
	"fmt"
)

func PrepareDealerOutputs(pub *PublicInput, prv *PrivateInput, dealerIndex int) (*DealerToCom1Message, *DealerToCom3Message, error) {
	dealerMsg, err := prepareDealerMessage(pub, prv, dealerIndex)
	if err != nil {
		return nil, nil, err
	}
	dealerToCom3, err := performDealerToCom3(pub, dealerMsg)
	if err != nil {
		return nil, nil, err
	}
	dealerToCom1 := &DealerToCom1Message{
		DealerID:       dealerMsg.DealerID,
		RowProjections: dealerMsg.RowProjections,
		ColProjections: dealerMsg.ColProjections,
	}
	return dealerToCom1, dealerToCom3, nil
}

func prepareDealerMessage(pub *PublicInput, prv *PrivateInput, dealerIndex int) (*DealerMessage, error) {
	if err := pub.Validate(); err != nil {
		return nil, err
	}
	if dealerIndex < 0 || dealerIndex >= len(pub.Committees.Dealers) {
		return nil, fmt.Errorf("dealer index %d out of range", dealerIndex)
	}

	f := pub.Field
	poly := NewTriPoly(pub.VSSParams.D)
	if err := poly.Randomize(f, prv.Secret); err != nil {
		return nil, fmt.Errorf("dealer %d failed to randomize polynomial: %w", prv.ID, err)
	}

	n := pub.VSSParams.N
	rowProj := make([]DealerProjection, n)
	colProj := make([]DealerProjection, n)
	futurePayloads := make([]DealerProjection, n)

	for idx := 0; idx < n; idx++ {
		scalar := uint64(idx + 1)
		rowMatrix := poly.ProjectFixX(f, scalar)
		colMatrix := poly.ProjectFixY(f, scalar)
		rowProj[idx] = DealerProjection{Index: idx + 1, Matrix: rowMatrix}
		colProj[idx] = DealerProjection{Index: idx + 1, Matrix: colMatrix}
		futurePayloads[idx] = DealerProjection{Index: idx + 1, Matrix: colMatrix}
	}

	return &DealerMessage{
		DealerID:       prv.ID,
		RowProjections: rowProj,
		ColProjections: colProj,
		FuturePayloads: futurePayloads,
	}, nil
}

func performDealerToCom3(pub *PublicInput, deal *DealerMessage) (*DealerToCom3Message, error) {
	if err := pub.Validate(); err != nil {
		return nil, err
	}
	if deal == nil {
		return nil, fmt.Errorf("missing dealer message")
	}
	com3IDs := pub.Committees.Com3
	if len(com3IDs) == 0 {
		return nil, fmt.Errorf("no Com3 receivers configured")
	}

	f := pub.Field
	payloadCount := len(deal.FuturePayloads)
	shareByPayload := make([][][][]uint64, payloadCount)
	for idx := 0; idx < payloadCount; idx++ {
		payload := deal.FuturePayloads[idx]
		matrices, err := shareFutureMatrixByGenerateShares(f, &pub.VSSParams, payload.Matrix)
		if err != nil {
			return nil, fmt.Errorf(
				"dealer %d failed to share future payload for com1 seq %d: %w",
				deal.DealerID, payload.Index, err,
			)
		}
		shareByPayload[idx] = matrices
	}

	shares := make([]DealerColumnShare, 0, payloadCount*len(com3IDs))
	for payloadIdx, payload := range deal.FuturePayloads {
		com1Seq := payload.Index
		if com1Seq <= 0 || com1Seq > len(pub.Committees.Com1) {
			return nil, fmt.Errorf("invalid com1 sequence %d", com1Seq)
		}
		matrices := shareByPayload[payloadIdx]
		for idx := range com3IDs {
			shares = append(shares, DealerColumnShare{
				DealerID: deal.DealerID,
				Com1Seq:  com1Seq,
				Com3Seq:  idx + 1,
				Matrix:   matrices[idx],
			})
		}
	}
	return &DealerToCom3Message{DealerID: deal.DealerID, Shares: shares}, nil
}

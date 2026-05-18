package fdvss

import (
	"fmt"
)

type Com4Result struct {
	DealerID int
	Index    uint64
	Value    uint64
}

func PerformCom4(
	pub *PublicInput,
	prv *PrivateInput,
	com3Msgs []Com3Message,
	com1ToCom4Msgs []Com1ToCom4Message,
) ([]Com4Result, error) {
	if pub == nil || prv == nil {
		return nil, fmt.Errorf("invalid inputs")
	}
	if err := pub.Validate(); err != nil {
		return nil, err
	}
	indices := pub.Committees.Indices(prv.ID)
	if indices.Com4 < 0 {
		return nil, fmt.Errorf("party %d is not in Com4", prv.ID)
	}

	workset, err := prepareCom4Workset(pub, com3Msgs)
	if err != nil {
		return nil, err
	}

	additionalDQ, err := applyCom4PublicComputation(pub, workset)
	if err != nil {
		return nil, err
	}
	for d := range additionalDQ {
		workset.disqualified[d] = struct{}{}
	}

	recipientSeq := indices.Com4 + 1
	indexScalar := uint64(recipientSeq)
	indexes := buildCom4Indexes(workset)
	com1MsgIndex := indexCom1ToCom4MsgsByDealer(com1ToCom4Msgs)

	results := make([]Com4Result, 0, len(pub.Committees.Dealers))
	for _, dealerID := range pub.Committees.Dealers {
		var value uint64

		if _, bad := workset.disqualified[dealerID]; bad {
			value = uint64(0)
		} else {
			dealerValue, err := buildAndReconstructRecipientShareForDealer(pub, workset, indexes, com1MsgIndex, recipientSeq, dealerID)
			if err != nil {
				return nil, fmt.Errorf("failed to reconstruct share for dealer %d: %w", dealerID, err)
			}
			value = dealerValue
		}
		results = append(results, Com4Result{
			DealerID: dealerID,
			Index:    indexScalar,
			Value:    value,
		})
	}

	return results, nil
}

type com4Workset struct {
	unhappyByDealer map[int]map[int]struct{}

	disqualified map[int]struct{}

	regularColumns []FutureColumn
	crossColumns   []FutureCrossColumn
}

type com4Indexes struct {
	regular map[int]map[int]*FutureColumn
	cross   map[int]map[int]map[int]*FutureCrossColumn
}

type regularSpecCacheKey struct {
	dealerID int
	ownerSeq int
	peerSeq  int
}

func prepareCom4Workset(pub *PublicInput, com3Msgs []Com3Message) (*com4Workset, error) {
	if err := pub.Validate(); err != nil {
		return nil, err
	}

	disqualifyCounts := make(map[int]int)

	unhappyCounts := make(map[int]map[int]int)
	for _, msg := range com3Msgs {
		for _, item := range msg.BroadcastData {
			dealerID := item.DealerID
			if item.Disqualified {
				disqualifyCounts[dealerID]++
			}
			if unhappyCounts[dealerID] == nil {
				unhappyCounts[dealerID] = make(map[int]int)
			}

			for _, com1Seq := range item.Unhappy {
				unhappyCounts[dealerID][com1Seq]++
			}
		}
	}

	threshold := 2*pub.VSSParams.D + 1
	disqualified := make(map[int]struct{})
	for dealerID, count := range disqualifyCounts {
		if count >= threshold {
			disqualified[dealerID] = struct{}{}
		}
	}

	unhappyByDealer := make(map[int]map[int]struct{})
	for dealerID, counts := range unhappyCounts {
		unhappySet := make(map[int]struct{})
		for com1Seq, count := range counts {
			if count >= threshold {
				unhappySet[com1Seq] = struct{}{}
			}
		}
		if len(unhappySet) > 0 {
			unhappyByDealer[dealerID] = unhappySet

			if len(unhappySet) > pub.VSSParams.D {
				disqualified[dealerID] = struct{}{}
			}
		}
	}

	filteredMsgs := filterCom3Messages(com3Msgs, disqualified)
	regularColumns, crossColumns, err := ReconstructFutureColumns(pub, filteredMsgs, unhappyByDealer)
	if err != nil {
		return nil, err
	}
	ws := &com4Workset{
		unhappyByDealer: unhappyByDealer,
		disqualified:    disqualified,
		regularColumns:  regularColumns,
		crossColumns:    crossColumns,
	}
	return ws, nil
}

func applyCom4PublicComputation(pub *PublicInput, ws *com4Workset) (map[int]struct{}, error) {
	result := make(map[int]struct{})
	if pub == nil || ws == nil {
		return result, fmt.Errorf("invalid public computation input")
	}
	if err := pub.Validate(); err != nil {
		return result, err
	}
	threshold := 2*pub.VSSParams.D + 1
	indexes := buildCom4Indexes(ws)
	specCache := make(map[regularSpecCacheKey][]uint64)

	n := len(pub.Committees.Com1)
	for _, dealerID := range pub.Committees.Dealers {
		if _, bad := ws.disqualified[dealerID]; bad {
			result[dealerID] = struct{}{}
			continue
		}
		unhappy := ws.unhappyByDealer[dealerID]
		if len(unhappy) == 0 {
			continue
		}
		for owner := range unhappy {
			matchCount := 0
			for peer := 1; peer <= n; peer++ {
				if _, isUnhappy := unhappy[peer]; isUnhappy {
					continue
				}
				ok := isDealerCrossConsistent(pub.Field, indexes, dealerID, owner, peer, specCache)
				if ok {
					matchCount++
				}
			}
			if matchCount < threshold {
				result[dealerID] = struct{}{}
				break
			}
		}
	}
	return result, nil
}

func buildCom4Indexes(ws *com4Workset) com4Indexes {
	out := com4Indexes{
		regular: make(map[int]map[int]*FutureColumn),
		cross:   make(map[int]map[int]map[int]*FutureCrossColumn),
	}
	for i := range ws.regularColumns {
		col := &ws.regularColumns[i]
		if out.regular[col.DealerID] == nil {
			out.regular[col.DealerID] = make(map[int]*FutureColumn)
		}
		out.regular[col.DealerID][col.Com1Seq] = col
	}
	for i := range ws.crossColumns {
		col := &ws.crossColumns[i]
		if out.cross[col.DealerID] == nil {
			out.cross[col.DealerID] = make(map[int]map[int]*FutureCrossColumn)
		}
		if out.cross[col.DealerID][col.Com1Seq] == nil {
			out.cross[col.DealerID][col.Com1Seq] = make(map[int]*FutureCrossColumn)
		}
		out.cross[col.DealerID][col.Com1Seq][col.PeerCom1] = col
	}
	return out
}

func isDealerCrossConsistent(
	f *FieldParams,
	indexes com4Indexes,
	dealerID, ownerSeq, peerSeq int,
	specCache map[regularSpecCacheKey][]uint64,
) bool {
	dealerRegular := indexes.regular[dealerID]
	if dealerRegular == nil {
		return false
	}
	regularCol, ok := dealerRegular[ownerSeq]
	if !ok {
		return false
	}
	dealerCross := indexes.cross[dealerID]
	if dealerCross == nil || dealerCross[ownerSeq] == nil {
		return false
	}
	crossCol, ok := dealerCross[ownerSeq][peerSeq]
	if !ok {
		return false
	}
	key := regularSpecCacheKey{dealerID: dealerID, ownerSeq: ownerSeq, peerSeq: peerSeq}
	fdCoeffs, ok := specCache[key]
	if !ok {
		fdCoeffs = specializeCoefficients(f, regularCol.Matrix, uint64(peerSeq))
		specCache[key] = fdCoeffs
	}
	return equalCoeffs(fdCoeffs, crossCol.Coeffs)
}

func filterCom3Messages(msgs []Com3Message, disqualified map[int]struct{}) []Com3Message {
	if len(disqualified) == 0 {
		return msgs
	}
	filtered := make([]Com3Message, 0, len(msgs))
	for _, msg := range msgs {
		filteredMsg := Com3Message{BroadcastData: make([]Com3BroadcastData, 0, len(msg.BroadcastData))}
		for _, item := range msg.BroadcastData {
			if _, bad := disqualified[item.DealerID]; bad {
				continue
			}
			filteredMsg.BroadcastData = append(filteredMsg.BroadcastData, item)
		}
		filtered = append(filtered, filteredMsg)
	}
	return filtered
}

func evalBivariateAtXZeroThenZK(f *FieldParams, matrix [][]uint64, zPoint uint64) (uint64, error) {
	if len(matrix) == 0 || len(matrix[0]) == 0 {
		return 0, fmt.Errorf("invalid bivariate coefficient matrix")
	}
	zCoeffs := make([]uint64, len(matrix[0]))
	copy(zCoeffs, matrix[0])
	return evaluatePolynomial(f, zCoeffs, zPoint), nil
}

func buildAndReconstructRecipientShareForDealer(
	pub *PublicInput,
	ws *com4Workset,
	indexes com4Indexes,
	com1MsgIndex map[int]map[int]*Com1ToCom4Message,
	recipientSeq int,
	dealerID int,
) (uint64, error) {
	if pub == nil || ws == nil {
		return 0, fmt.Errorf("invalid inputs for column reconstruction")
	}

	zPoint := uint64(recipientSeq)

	msgByCom1Seq := com1MsgIndex[dealerID]

	points := make([]lagrangePoint, 0, len(pub.Committees.Com1))
	for com1Seq := 1; com1Seq <= len(pub.Committees.Com1); com1Seq++ {

		msg, hasMsg := msgByCom1Seq[com1Seq]

		value := uint64(0)

		ownerUnhappyOnDealer := false
		if set, ok := ws.unhappyByDealer[dealerID]; ok {
			_, ownerUnhappyOnDealer = set[com1Seq]
		}

		if ownerUnhappyOnDealer {

			futureByCom1Seq := indexes.regular[dealerID]
			if futureByCom1Seq == nil {
				return 0, fmt.Errorf("missing future columns for dealer %d", dealerID)
			}
			futureCol, ok := futureByCom1Seq[com1Seq]
			if !ok {
				return 0, fmt.Errorf("missing future column for dealer %d com1 seq %d", dealerID, com1Seq)
			}

			term, err := evalBivariateAtXZeroThenZK(pub.Field, futureCol.Matrix, zPoint)
			if err != nil {
				return 0, fmt.Errorf("invalid future column matrix for dealer %d com1 seq %d: %w", dealerID, com1Seq, err)
			}
			value = term
		} else {

			if !hasMsg {
				return 0, fmt.Errorf("missing Com1ToCom4 message for dealer %d com1 seq %d", dealerID, com1Seq)
			}
			if len(msg.Coeffs) == 0 {
				return 0, fmt.Errorf("empty coefficients from com1 seq %d for dealer %d", com1Seq, dealerID)
			}
			term := msg.Coeffs[0]
			value = term
		}

		x := uint64(com1Seq)
		points = append(points, lagrangePoint{
			x: x,
			y: value,
		})
	}
	if len(points) <= pub.VSSParams.D {
		return 0, fmt.Errorf("insufficient points for interpolation: have %d need %d", len(points), pub.VSSParams.D+1)
	}

	return reedSolomonDecode(pub.Field, points, pub.VSSParams.D)
}

func indexCom1ToCom4MsgsByDealer(msgs []Com1ToCom4Message) map[int]map[int]*Com1ToCom4Message {
	out := make(map[int]map[int]*Com1ToCom4Message)
	for i := range msgs {
		msg := &msgs[i]
		if out[msg.DealerID] == nil {
			out[msg.DealerID] = make(map[int]*Com1ToCom4Message)
		}
		out[msg.DealerID][msg.Com1ID] = msg
	}
	return out
}

func equalCoeffs(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

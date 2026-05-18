package fdvss

import (
	"fmt"
	"sort"
	"sync"
)

type dealerStatus struct {
	happy        map[int]struct{}
	unhappy      map[int]struct{}
	disqualified bool
}

type consistencyGraph struct {
	n     int
	edges []bool
}

func (g *consistencyGraph) edge(i, j int) bool {
	return g.edges[i*g.n+j]
}

func (g *consistencyGraph) setEdge(i, j int, v bool) {
	g.edges[i*g.n+j] = v
	g.edges[j*g.n+i] = v
}

type pairKey struct {
	a int
	b int
}

type com2PairShare struct {
	com2Seq int
	left    []uint64
	right   []uint64
}

var com3LagrangePointBufPool = sync.Pool{
	New: func() any {
		return make([]lagrangePoint, 0, 128)
	},
}

func borrowLagrangePointBuf(need int) []lagrangePoint {
	buf := com3LagrangePointBufPool.Get().([]lagrangePoint)
	if cap(buf) < need {
		com3LagrangePointBufPool.Put(buf[:0])
		return make([]lagrangePoint, need)
	}
	return buf[:need]
}

func releaseLagrangePointBuf(buf []lagrangePoint) {
	if cap(buf) == 0 {
		return
	}
	com3LagrangePointBufPool.Put(buf[:0])
}

func PerformCom3(
	pub *PublicInput,
	prv *PrivateInput,
	com2Msgs []Com2Message,
	dealerShares []DealerToCom3Message,
	com1ToCom3 []Com1ToCom3Message,
) (*Com3Message, error) {
	if err := pub.Validate(); err != nil {
		return nil, err
	}
	indices := pub.Committees.Indices(prv.ID)
	if indices.Com3 < 0 {
		return nil, fmt.Errorf("party %d is not in Com3", prv.ID)
	}

	cliqueThreshold := 2*pub.VSSParams.D + 1
	statuses, _, err := computeDealerStatusesFromCom2(pub, com2Msgs, cliqueThreshold)
	if err != nil {
		return nil, err
	}
	unhappyByDealer := make(map[int][]int, len(statuses))
	broadcast := make([]Com3BroadcastData, 0, len(statuses))
	com3Seq := indices.Com3 + 1
	forwardDealer := collectForwardDealerShares(pub, com3Seq, statuses, dealerShares)
	forwardCom1 := collectForwardCom1Shares(pub, com3Seq, statuses, com1ToCom3)
	forwardDealerByDealer := groupDealerSharesByDealer(forwardDealer)
	forwardCom1ByDealer := groupCom1SharesByDealer(forwardCom1)
	for dealerID, st := range statuses {
		if st == nil {
			continue
		}
		if st.disqualified {
			unhappyByDealer[dealerID] = []int{}
		} else {
			unhappyByDealer[dealerID] = setToSortedSlice(st.unhappy)
		}
		item := Com3BroadcastData{
			DealerID:     dealerID,
			Unhappy:      unhappyByDealer[dealerID],
			Disqualified: st.disqualified,
		}
		if !st.disqualified {
			item.DealerShares = forwardDealerByDealer[dealerID]
			item.Com1PeerShares = forwardCom1ByDealer[dealerID]
		}
		broadcast = append(broadcast, item)
	}

	return &Com3Message{
		BroadcastData: broadcast,
	}, nil
}

func computeDealerStatusesFromCom2(pub *PublicInput, msgs []Com2Message, threshold int) (map[int]*dealerStatus, []int, error) {

	if pub != nil && pub.Field != nil {
		d := pub.VSSParams.D
		xsWarm := make([]uint64, d+1)
		for i := range xsWarm {
			xsWarm[i] = uint64(i + 1)
		}
		_ = WarmNewtonInterpolationCacheForXs(pub.Field, xsWarm, d)
	}

	byDealer := make(map[int]map[pairKey][]com2PairShare)
	for msgIdx, msg := range msgs {
		com2Seq := msgIdx + 1
		if com2Seq > len(pub.Committees.Com2) {
			continue
		}
		for _, item := range msg.BroadcastData {
			if byDealer[item.DealerID] == nil {
				byDealer[item.DealerID] = make(map[pairKey][]com2PairShare)
			}
			for _, p := range item.Pairs {
				if p.Com1A <= 0 || p.Com1B <= 0 {
					continue
				}
				k := pairKey{a: p.Com1A, b: p.Com1B}
				if p.Com1A > p.Com1B {
					k = pairKey{a: p.Com1B, b: p.Com1A}
				}
				byDealer[item.DealerID][k] = append(byDealer[item.DealerID][k], com2PairShare{
					com2Seq: com2Seq,
					left:    p.LeftDiff,
					right:   p.RightDiff,
				})
			}
		}
	}

	statuses := make(map[int]*dealerStatus, len(byDealer))
	disqualified := make([]int, 0)
	n := len(pub.Committees.Com1)
	sharedEdges := make([]bool, n*n)
	graph := consistencyGraph{n: n, edges: sharedEdges}
	for dealerID, pairShares := range byDealer {
		for k := range sharedEdges {
			sharedEdges[k] = false
		}
		for i := 0; i < n; i++ {
			sharedEdges[i*n+i] = true
		}

		for i := 1; i <= n; i++ {
			for j := i + 1; j <= n; j++ {
				k := pairKey{a: i, b: j}
				zero, err := reconstructPairZero(pub.Field, pairShares[k], pub.VSSParams.D, threshold)
				if err != nil {
					return nil, nil, fmt.Errorf("dealer %d pair (%d,%d): %w", dealerID, i, j, err)
				}
				if zero {
					graph.setEdge(i-1, j-1, true)
				}
			}
		}

		clique := findCliqueIDs(&graph, threshold)
		st := &dealerStatus{
			happy:   make(map[int]struct{}),
			unhappy: make(map[int]struct{}),
		}
		if len(clique) < threshold {
			st.disqualified = true
			disqualified = append(disqualified, dealerID)
			statuses[dealerID] = st
			continue
		}
		for _, id := range clique {
			st.happy[id] = struct{}{}
		}
		for i := 1; i <= n; i++ {
			if _, ok := st.happy[i]; !ok {
				st.unhappy[i] = struct{}{}
			}
		}
		statuses[dealerID] = st
	}
	return statuses, disqualified, nil
}

func reconstructPairZero(f *FieldParams, shares []com2PairShare, degree, threshold int) (bool, error) {
	if len(shares) < threshold {
		return false, nil
	}
	leftSize := 0
	rightSize := 0
	for _, s := range shares {
		if len(s.left) > leftSize {
			leftSize = len(s.left)
		}
		if len(s.right) > rightSize {
			rightSize = len(s.right)
		}
	}
	leftZero, err := reconstructCoeffVectorZero(f, shares, leftSize, degree, true)
	if err != nil {
		return false, err
	}
	rightZero, err := reconstructCoeffVectorZero(f, shares, rightSize, degree, false)
	if err != nil {
		return false, err
	}
	return leftZero && rightZero, nil
}

func reconstructCoeffVectorZero(f *FieldParams, shares []com2PairShare, coeffSize, degree int, isLeft bool) (bool, error) {
	if coeffSize <= 0 {
		return true, nil
	}
	maxSeq := 0
	for _, s := range shares {
		if s.com2Seq > maxSeq {
			maxSeq = s.com2Seq
		}
	}
	if maxSeq <= 0 {
		return false, nil
	}

	lastBySeq := make([][]uint64, maxSeq+1)
	for _, s := range shares {
		coeffs := s.right
		if isLeft {
			coeffs = s.left
		}
		seq := s.com2Seq
		if seq <= 0 || seq > maxSeq {
			continue
		}
		lastBySeq[seq] = coeffs
	}

	seqs := make([]int, 0, len(shares))
	allFullLen := true
	for seq := 1; seq <= maxSeq; seq++ {
		v := lastBySeq[seq]
		if v == nil {
			continue
		}
		if len(v) < coeffSize {
			allFullLen = false
			break
		}
		seqs = append(seqs, seq)
	}

	if allFullLen && len(seqs) >= degree+1 {
		points := borrowLagrangePointBuf(len(seqs))
		defer releaseLagrangePointBuf(points)
		r, err := RandomNonZeroMod(f)
		if err != nil {
			return false, fmt.Errorf("random linear combo: %w", err)
		}
		for i, seq := range seqs {
			v := lastBySeq[seq]
			rp := Uint64One()
			acc := Uint64Zero()
			for c := 0; c < coeffSize; c++ {
				acc = f.ModAddCanon(acc, f.ModMulCanon(rp, v[c]))
				rp = f.ModMulCanon(rp, r)
			}
			points[i] = lagrangePoint{x: uint64(seq), y: acc}
		}
		value, err := reedSolomonDecodeSortedStrict(f, points, degree)
		if err != nil || value != 0 {
			return false, nil
		}
		return true, nil
	}

	seenEpoch := make([]int, maxSeq+1)
	posBySeq := make([]int, maxSeq+1)
	orderedBuf := make([]lagrangePoint, 0, maxSeq)
	epoch := 0
	for coeffIdx := 0; coeffIdx < coeffSize; coeffIdx++ {
		epoch++
		points := make([]lagrangePoint, 0, len(shares))
		for _, s := range shares {
			coeffs := s.right
			if isLeft {
				coeffs = s.left
			}
			if coeffIdx >= len(coeffs) {
				continue
			}
			seq := s.com2Seq
			if seq <= 0 || seq > maxSeq {
				continue
			}
			if seenEpoch[seq] != epoch {
				seenEpoch[seq] = epoch
				posBySeq[seq] = len(points)
				points = append(points, lagrangePoint{x: uint64(seq), y: coeffs[coeffIdx]})
			} else {
				points[posBySeq[seq]].y = coeffs[coeffIdx]
			}
		}
		if len(points) < degree+1 {
			return false, nil
		}

		orderedBuf = orderedBuf[:0]
		for seq := 1; seq <= maxSeq; seq++ {
			if seenEpoch[seq] != epoch {
				continue
			}
			orderedBuf = append(orderedBuf, lagrangePoint{x: uint64(seq), y: points[posBySeq[seq]].y})
		}
		value, err := reedSolomonDecodeSortedStrict(f, orderedBuf, degree)
		if err != nil || value != 0 {
			return false, nil
		}
	}
	return true, nil
}

func findCliqueIDs(graph *consistencyGraph, threshold int) []int {
	size := graph.n
	if size == 0 {
		return nil
	}

	degreeThreshold := threshold - 1

	degrees := make([]int, size)
	for i := 0; i < size; i++ {
		base := i * size
		for j := 0; j < size; j++ {
			if i != j && graph.edges[base+j] {
				degrees[i]++
			}
		}
	}

	highDegreeVertices := make([]int, 0)
	for i := 0; i < size; i++ {
		if degrees[i] >= degreeThreshold {
			highDegreeVertices = append(highDegreeVertices, i)
		}
	}

	if len(highDegreeVertices) < threshold {
		return nil
	}

	for i := 0; i < len(highDegreeVertices); i++ {
		for j := i + 1; j < len(highDegreeVertices); j++ {
			vi := highDegreeVertices[i]
			vj := highDegreeVertices[j]
			if !graph.edge(vi, vj) {
				return nil
			}
		}
	}

	ids := make([]int, len(highDegreeVertices))
	for idx, pos := range highDegreeVertices {
		ids[idx] = pos + 1
	}
	return ids
}

func collectForwardDealerShares(
	pub *PublicInput,
	com3Seq int,
	statuses map[int]*dealerStatus,
	dealerMsgs []DealerToCom3Message,
) []DealerColumnShare {
	var out []DealerColumnShare
	for _, msg := range dealerMsgs {
		status, ok := statuses[msg.DealerID]
		if !ok || status == nil {
			continue
		}

		if status.disqualified {
			continue
		}
		if len(status.unhappy) == 0 {

			continue
		}
		for _, share := range msg.Shares {

			if share.Com3Seq != com3Seq {
				continue
			}

			if _, bad := status.unhappy[share.Com1Seq]; !bad {
				continue
			}
			out = append(out, share)
		}
	}
	return out
}

func collectForwardCom1Shares(
	pub *PublicInput,
	com3Seq int,
	statuses map[int]*dealerStatus,
	com1Msgs []Com1ToCom3Message,
) []Com1ColumnShare {
	var out []Com1ColumnShare
	for _, msg := range com1Msgs {
		share := msg.Share

		if share.Com3Seq != com3Seq {
			continue
		}
		status, ok := statuses[share.DealerID]
		if !ok || status == nil {
			continue
		}

		if status.disqualified {
			continue
		}
		if len(status.unhappy) == 0 {
			continue
		}

		if _, ownerUnhappy := status.unhappy[share.Com1Seq]; ownerUnhappy {
			continue
		}
		if _, peerUnhappy := status.unhappy[share.PeerIndex]; !peerUnhappy {
			continue
		}
		out = append(out, share)
	}
	return out
}

func setToSortedSlice(set map[int]struct{}) []int {
	out := make([]int, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Ints(out)
	return out
}

func groupDealerSharesByDealer(shares []DealerColumnShare) map[int][]DealerColumnShare {
	out := make(map[int][]DealerColumnShare)
	for _, share := range shares {
		out[share.DealerID] = append(out[share.DealerID], share)
	}
	return out
}

func groupCom1SharesByDealer(shares []Com1ColumnShare) map[int][]Com1ColumnShare {
	out := make(map[int][]Com1ColumnShare)
	for _, share := range shares {
		out[share.DealerID] = append(out[share.DealerID], share)
	}
	return out
}

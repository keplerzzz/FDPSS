package fdvss

import (
	"fmt"
)

type com2Eval struct {
	row [][]uint64
	col [][]uint64
}

type com2SpecializeCacheKey struct {
	com1Seq int
	scalar  int
	isRow   bool
}

func PerformCom2(pub *PublicInput, prv *PrivateInput, msgs []Com1ToCom2Message) (*Com2Message, error) {
	if err := pub.Validate(); err != nil {
		return nil, err
	}
	f := pub.Field
	indices := pub.Committees.Indices(prv.ID)
	if indices.Com2 < 0 {
		return nil, fmt.Errorf("party %d is not in Com2", prv.ID)
	}
	targetID := pub.Committees.Com2[indices.Com2]
	byDealer := make(map[int]map[int]com2Eval)
	for _, msg := range msgs {
		if msg.Target != targetID {
			continue
		}
		if msg.Com1ID <= 0 {
			return nil, fmt.Errorf("invalid Com1 sequence %d", msg.Com1ID)
		}
		if byDealer[msg.DealerID] == nil {
			byDealer[msg.DealerID] = make(map[int]com2Eval)
		}
		byDealer[msg.DealerID][msg.Com1ID] = com2Eval{
			row: msg.RowShare,
			col: msg.ColShare,
		}
	}

	out := make([]Com2BroadcastData, 0, len(byDealer))
	n := len(pub.Committees.Com1)
	for dealerID, evals := range byDealer {
		pairs := make([]Com2PairBroadcast, 0, n*(n-1)/2)
		specCache := make(map[com2SpecializeCacheKey][]uint64, n*n*2)
		getSpecialized := func(com1Seq, scalar int, isRow bool) []uint64 {
			key := com2SpecializeCacheKey{com1Seq: com1Seq, scalar: scalar, isRow: isRow}
			if cached, ok := specCache[key]; ok {
				return cached
			}
			e := evals[com1Seq]
			src := e.col
			if isRow {
				src = e.row
			}
			result := specializeCoefficients(f, src, uint64(scalar))
			specCache[key] = result
			return result
		}
		for i := 1; i <= n; i++ {
			if _, okI := evals[i]; !okI {
				continue
			}
			for ip := i + 1; ip <= n; ip++ {
				if _, okIP := evals[ip]; !okIP {
					continue
				}
				left := diffSlice(f,
					getSpecialized(i, ip, true),
					getSpecialized(ip, i, false),
				)
				right := diffSlice(f,
					getSpecialized(i, ip, false),
					getSpecialized(ip, i, true),
				)
				pairs = append(pairs, Com2PairBroadcast{
					Com1A:     i,
					Com1B:     ip,
					LeftDiff:  left,
					RightDiff: right,
				})
			}
		}
		out = append(out, Com2BroadcastData{DealerID: dealerID, Pairs: pairs})
	}

	return &Com2Message{BroadcastData: out}, nil
}

func diffSlice(f *FieldParams, a, b []uint64) []uint64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	out := make([]uint64, n)
	for i := 0; i < n; i++ {
		out[i] = f.ModSubCanon(a[i], b[i])
	}
	return out
}

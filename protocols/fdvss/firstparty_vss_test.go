package fdvss

import (
	"flag"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"go-fdvss-fdpss/communication/fake"
	"go-fdvss-fdpss/msgpack"
	"go-fdvss-fdpss/primitives"

	"github.com/stretchr/testify/require"
)

var (
	benchN      = flag.Int("N", 0, "VSS parameter N (committee size). If not set, reads from N environment variable or defaults to 7")
	benchDegree = flag.Int("degree", -1, "VSS polynomial degree D (or env FDVSS_D). If negative, use (N-1)/3; requires N >= 3*D+1")
	benchFieldP = flag.Uint64("field-p", 0, "Required prime modulus P; pass after -args; validated via NewFieldParams")
)

func TestFDVSSFirstPartyAvg(t *testing.T) {
	require := require.New(t)

	var (
		n = MustResolveBenchN(*benchN)
		d = MustResolveBenchD(n, *benchDegree)
	)
	field, err := ResolveBenchFieldP(*benchFieldP)
	require.NoError(err, "must pass -field-p=<prime> after go test -args")

	const numRuns = 10

	type runStats struct {
		dealerSize int64
		com1Size   int64
		com2Size   int64
		com3Size   int64
		com4Size   int64
	}
	allStats := make([]runStats, numRuns)

	calcSize := func(v interface{}) int64 { return msgpack.VarintScalarWireSize(v) }

	for run := 0; run < numRuns; run++ {

		pub, dealerPrv, com1Prvs, com2Prvs, com3Prvs, com4Prvs, orch, secret := SetupBenchSingleDealerNetwork(t, n, d, field, 12345)

		flow := &FDVSSIOFlow{
			Com1ToCom2ByCom2: make([][]Com1ToCom2Message, len(com2Prvs)),
			Com1ToCom3ByCom3: make([][]Com1ToCom3Message, len(com3Prvs)),
			Com1ToCom4ByCom4: make([][]Com1ToCom4Message, len(com4Prvs)),
			Com2Broadcasts:   make([]Com2Message, len(com2Prvs)),
			Com3Broadcasts:   make([]Com3Message, len(com3Prvs)),
		}

		dealerToCom1, dealerToCom3, err := PrepareDealerOutputs(pub, dealerPrv, 0)
		require.NoError(err)

		var dealerP2PSize int64
		for i, com1ID := range pub.Committees.Com1 {
			entry, e := buildCom1EntryFromDealerToCom1(pub, dealerToCom1, com1ID, i+1)
			require.NoError(e)
			dealerP2PSize += calcSize(entry)
		}
		dealerP2PSize += calcSize(dealerToCom3)
		allStats[run].dealerSize = dealerP2PSize

		dealerMsgForCom1 := &DealerMessage{
			DealerID:       dealerToCom1.DealerID,
			RowProjections: dealerToCom1.RowProjections,
			ColProjections: dealerToCom1.ColProjections,
			FuturePayloads: nil,
		}
		require.NoError(SendDealerToCom1Batch(pub, dealerPrv, dealerMsgForCom1, pub.Committees.Com1))
		require.NoError(SendDealerToCom3Batch(dealerPrv, dealerToCom3, pub.Committees.Com3))

		flow.DealerToCom1 = dealerToCom1
		flow.DealerToCom3 = dealerToCom3
		flow.Stats.DealerSize = allStats[run].dealerSize

		reconstructedSecrets, err := continueFDVSSFromCom1(pub, dealerPrv, com1Prvs, com2Prvs, com3Prvs, com4Prvs, orch, flow)
		require.NoError(err)

		allStats[run].com1Size = flow.Stats.Com1Size
		allStats[run].com2Size = flow.Stats.Com2Size
		allStats[run].com3Size = flow.Stats.Com3Size
		allStats[run].com4Size = flow.Stats.Com4Size

		if len(reconstructedSecrets) > 0 {
			dealerID := pub.Committees.Dealers[0]
			recovered, ok := reconstructedSecrets[dealerID]
			require.True(ok)
			require.True(Uint64Equal(recovered, secret), "secret mismatch run %d", run+1)
		}
	}

	var sumDealer, sumCom1, sumCom2, sumCom3, sumCom4 int64
	for _, s := range allStats {
		sumDealer += s.dealerSize
		sumCom1 += s.com1Size
		sumCom2 += s.com2Size
		sumCom3 += s.com3Size
		sumCom4 += s.com4Size
	}

	fmt.Printf("avg size (bytes, n=%d d=%d runs=%d): dealer=%d com1=%d com2=%d com3=%d com4=%d\n",
		n, d, numRuns,
		sumDealer/numRuns, sumCom1/numRuns, sumCom2/numRuns, sumCom3/numRuns, sumCom4/numRuns)
}

type FirstPartyRoundStats struct {
	DealerSize int64
	Com1Size   int64
	Com2Size   int64
	Com3Size   int64
	Com4Size   int64
}

type FDVSSIOFlow struct {
	DealerToCom1      *DealerToCom1Message
	DealerToCom3      *DealerToCom3Message
	Com1ToCom2ByCom2  [][]Com1ToCom2Message
	Com1ToCom3ByCom3  [][]Com1ToCom3Message
	Com1ToCom4ByCom4  [][]Com1ToCom4Message
	Com2Broadcasts    []Com2Message
	Com3Broadcasts    []Com3Message
	Com4ResultMessage []Com4ResultMessage
	Stats             FirstPartyRoundStats
}

func broadcastPartyIDs(orch *fake.Orchestrator) []int {
	ids := make([]int, 0, len(orch.Channels))
	for id := range orch.Channels {
		ids = append(ids, id)
	}
	return ids
}

func drainBroadcastOneIfPending(orch *fake.Orchestrator, partyID int) {
	if pbc, ok := orch.Channels[partyID]; ok {
		select {
		case <-pbc.ReceiveChannel:
		default:
		}
	}
}

func drainBroadcastOneIfPendingMany(orch *fake.Orchestrator, partyIDs []int) {
	for _, id := range partyIDs {
		drainBroadcastOneIfPending(orch, id)
	}
}

func partyIDsExcept(ids []int, exclude map[int]struct{}) []int {
	out := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, skip := exclude[id]; !skip {
			out = append(out, id)
		}
	}
	return out
}

func continueFDVSSFromCom1(
	pub *PublicInput,
	dealerPrv *PrivateInput,
	com1Prvs []PrivateInput,
	com2Prvs []PrivateInput,
	com3Prvs []PrivateInput,
	com4Prvs []PrivateInput,
	orch *fake.Orchestrator,
	flow *FDVSSIOFlow,
) (map[int]uint64, error) {

	calcSize := func(v interface{}) int64 { return msgpack.VarintScalarWireSize(v) }

	runCom1 := func(idx int) ([]Com1ToCom2Message, []Com1ToCom3Message, []Com1ToCom4Message, error) {
		prv := &com1Prvs[idx]
		entry, err := ReceiveDealerFromCom1Reliable(prv.P2P, dealerPrv.ID)
		if err != nil {
			return nil, nil, nil, err
		}
		toCom2, err := PerformCom1ToCom2(pub, prv, *entry)
		if err != nil {
			return nil, nil, nil, err
		}
		toCom3, err := PerformCom1ToCom3(pub, prv, *entry)
		if err != nil {
			return nil, nil, nil, err
		}
		toCom4, err := PerformCom1ToCom4(pub, prv, *entry)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := SendCom1ToCom2Batch(prv, toCom2); err != nil {
			return nil, nil, nil, err
		}
		if err := SendCom1ToCom3Batch(prv, toCom3, pub.Committees.Com3); err != nil {
			return nil, nil, nil, err
		}
		if err := SendCom1ToCom4Batch(prv, toCom4, pub.Committees.Com4); err != nil {
			return nil, nil, nil, err
		}
		return toCom2, toCom3, toCom4, nil
	}

	com1To2First, com1To3First, com1To4First, err := runCom1(0)
	if err != nil {
		return nil, err
	}
	for _, m := range com1To2First {
		flow.Stats.Com1Size += calcSize(m)
	}
	for _, m := range com1To3First {
		flow.Stats.Com1Size += calcSize(m)
	}
	for _, m := range com1To4First {
		flow.Stats.Com1Size += calcSize(m)
	}

	if err := runOthersParallel(len(com1Prvs), func(i int) error {
		if i == 0 {
			return nil
		}
		_, _, _, e := runCom1(i)
		return e
	}); err != nil {
		return nil, err
	}

	runCom2 := func(idx int) (*Com2Message, error) {
		prv := &com2Prvs[idx]
		recvMsgs, err := ReceiveCom1ToCom2All(prv.P2P, pub.Committees.Com1)
		if err != nil {
			return nil, err
		}
		flow.Com1ToCom2ByCom2[idx] = recvMsgs
		out, err := PerformCom2(pub, prv, recvMsgs)
		if err != nil {
			return nil, err
		}
		if err := BroadcastCom2(prv, out); err != nil {
			return nil, err
		}
		flow.Com2Broadcasts[idx] = *out
		return out, nil
	}

	com2First, err := runCom2(0)
	if err != nil {
		return nil, err
	}
	flow.Stats.Com2Size = calcSize(com2First)

	if err := runOthersParallel(len(com2Prvs), func(i int) error {
		if i == 0 {
			return nil
		}
		_, e := runCom2(i)
		return e
	}); err != nil {
		return nil, err
	}
	if err := deliverBroadcastRound(orch); err != nil {
		return nil, err
	}

	runCom3 := func(idx int) (*Com3Message, error) {
		prv := &com3Prvs[idx]
		recvCom2, err := ReceiveCom2Messages(prv.BC, pub.Committees.Com2)
		if err != nil {
			return nil, err
		}
		recvDealer, err := ReceiveDealerToCom3All(prv.P2P, pub.Committees.Dealers)
		if err != nil {
			return nil, err
		}
		recvCom1, err := ReceiveCom1ToCom3All(prv.P2P, pub.Committees.Com1)
		if err != nil {
			return nil, err
		}
		flow.Com1ToCom3ByCom3[idx] = recvCom1

		out, err := PerformCom3(pub, prv, recvCom2, recvDealer, recvCom1)
		if err != nil {
			return nil, err
		}
		if err := requireBroadcastChannel(prv); err != nil {
			return nil, err
		}
		if err := sendMessage(prv.BC, out); err != nil {
			return nil, err
		}
		flow.Com3Broadcasts[idx] = *out
		return out, nil
	}

	com3First, err := runCom3(0)
	if err != nil {
		return nil, err
	}
	flow.Stats.Com3Size = calcSize(com3First)

	if err := runOthersParallel(len(com3Prvs), func(i int) error {
		if i == 0 {
			return nil
		}
		_, e := runCom3(i)
		return e
	}); err != nil {
		return nil, err
	}
	exclCom3 := make(map[int]struct{})
	for _, id := range pub.Committees.Com3 {
		exclCom3[id] = struct{}{}
	}
	drainBroadcastOneIfPendingMany(orch, partyIDsExcept(broadcastPartyIDs(orch), exclCom3))

	if err := deliverBroadcastRound(orch); err != nil {
		return nil, err
	}

	flow.Com4ResultMessage = make([]Com4ResultMessage, len(com4Prvs))
	runCom4 := func(idx int) ([]Com4Result, error) {
		prv := &com4Prvs[idx]
		recvCom3, err := ReceiveCom3Messages(prv.BC, pub.Committees.Com3)
		if err != nil {
			return nil, err
		}
		recvCom1, err := ReceiveCom1ToCom4All(prv.P2P, pub.Committees.Com1)
		if err != nil {
			return nil, err
		}
		flow.Com1ToCom4ByCom4[idx] = recvCom1

		results, err := PerformCom4(pub, prv, recvCom3, recvCom1)
		if err != nil {
			return nil, err
		}
		if err := BroadcastCom4Result(prv, results); err != nil {
			return nil, err
		}
		flow.Com4ResultMessage[idx] = Com4ResultMessage{From: prv.ID, Results: results}
		return results, nil
	}

	com4First, err := runCom4(0)
	if err != nil {
		return nil, err
	}
	flow.Stats.Com4Size = calcSize(Com4ResultMessage{From: com4Prvs[0].ID, Results: com4First})

	if err := runOthersParallel(len(com4Prvs), func(i int) error {
		if i == 0 {
			return nil
		}
		_, e := runCom4(i)
		return e
	}); err != nil {
		return nil, err
	}
	exclCom4 := make(map[int]struct{})
	for _, id := range pub.Committees.Com4 {
		exclCom4[id] = struct{}{}
	}
	drainBroadcastOneIfPendingMany(orch, partyIDsExcept(broadcastPartyIDs(orch), exclCom4))

	if err := deliverBroadcastRound(orch); err != nil {
		return nil, err
	}

	com4Msgs, err := ReceiveCom4Results(dealerPrv.BC, pub.Committees.Com4)
	if err != nil {
		return nil, err
	}
	reconstructed, err := ReconstructSecret(pub, com4Msgs)
	if err != nil {
		return nil, err
	}
	return reconstructed, nil
}

func runOthersParallel(total int, fn func(i int) error) error {
	if total <= 1 {
		return nil
	}
	var (
		wg      sync.WaitGroup
		errOnce sync.Once
		retErr  error
		sem     = make(chan struct{}, runtime.NumCPU())
	)
	for i := 1; i < total; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := fn(idx); err != nil {
				errOnce.Do(func() { retErr = err })
			}
		}(i)
	}
	wg.Wait()
	return retErr
}

func deliverBroadcastRound(orch *fake.Orchestrator) error {
	if err := orch.ReceiveMessages(); err != nil {
		return err
	}
	return orch.Broadcast()
}

func buildCom1EntryFromProjections(dealerID int, rowProjs, colProjs []DealerProjection, com1Seq int) (*Com1Entry, error) {
	row, err := pickProjectionBySeq(rowProjs, com1Seq)
	if err != nil {
		return nil, fmt.Errorf("missing row projection: %w", err)
	}
	col, err := pickProjectionBySeq(colProjs, com1Seq)
	if err != nil {
		return nil, fmt.Errorf("missing col projection: %w", err)
	}
	return &Com1Entry{DealerID: dealerID, Com1ID: com1Seq, Row: *row, Col: *col}, nil
}

func buildCom1EntryFromDealerToCom1(_ *PublicInput, msg *DealerToCom1Message, _ int, com1Seq int) (*Com1Entry, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil dealer to com1 message")
	}
	return buildCom1EntryFromProjections(msg.DealerID, msg.RowProjections, msg.ColProjections, com1Seq)
}

func pickProjectionBySeq(projs []DealerProjection, seq int) (*DealerProjection, error) {
	for i := range projs {
		if projs[i].Index == seq {
			return &projs[i], nil
		}
	}
	return nil, fmt.Errorf("projection for seq=%d not found", seq)
}

func SetupBenchSingleDealerNetwork(
	t testing.TB,
	n, d int,
	field *FieldParams,
	dealerSecretConst int,
) (
	pub *PublicInput,
	dealerPrv *PrivateInput,
	com1Prvs []PrivateInput,
	com2Prvs []PrivateInput,
	com3Prvs []PrivateInput,
	com4Prvs []PrivateInput,
	o *fake.Orchestrator,
	secret uint64,
) {
	require := require.New(t)
	if field == nil {
		require.FailNow("SetupBenchSingleDealerNetwork: field is nil")
	}
	require.NoError(field.Validate())
	committees := Committees{Dealers: []int{0}, Com1: make([]int, n), Com2: make([]int, n), Com3: make([]int, n), Com4: make([]int, n)}
	for i := 0; i < n; i++ {
		committees.Com1[i] = 1 + i
		committees.Com2[i] = 1 + n + i
		committees.Com3[i] = 1 + 2*n + i
		committees.Com4[i] = 1 + 3*n + i
	}
	pub = &PublicInput{VSSParams: primitives.Params{N: n, D: d}, Committees: committees, Field: field}
	require.NoError(pub.Validate())
	orch := fake.NewOrchestrator()
	o = &orch
	totalParties := 1 + 4*n
	var bcChannels []fake.PartyBroadcastChannel
	var p2pChannels []fake.PartyPointToPointChannel
	for party := 0; party < totalParties; party++ {
		bcCh := fake.NewPartyBroadcastChannel(party)
		bcChannels = append(bcChannels, bcCh)
		o.AddChannel(bcCh)
		p2pCh := fake.NewPartyPointToPointChannel(party, o)
		p2pChannels = append(p2pChannels, p2pCh)
		o.AddP2PChannel(p2pCh)
	}
	secret = field.Uint64FromInt(dealerSecretConst)
	dealerPrv = &PrivateInput{ID: 0, Secret: secret, BC: bcChannels[0], P2P: p2pChannels[0]}
	com1Prvs = make([]PrivateInput, n)
	for i := 0; i < n; i++ {
		com1Prvs[i] = PrivateInput{ID: committees.Com1[i], BC: bcChannels[committees.Com1[i]], P2P: p2pChannels[committees.Com1[i]]}
	}
	com2Prvs = make([]PrivateInput, n)
	for i := 0; i < n; i++ {
		com2Prvs[i] = PrivateInput{ID: committees.Com2[i], BC: bcChannels[committees.Com2[i]], P2P: p2pChannels[committees.Com2[i]]}
	}
	com3Prvs = make([]PrivateInput, n)
	for i := 0; i < n; i++ {
		com3Prvs[i] = PrivateInput{ID: committees.Com3[i], BC: bcChannels[committees.Com3[i]], P2P: p2pChannels[committees.Com3[i]]}
	}
	com4Prvs = make([]PrivateInput, n)
	for i := 0; i < n; i++ {
		com4Prvs[i] = PrivateInput{ID: committees.Com4[i], BC: bcChannels[committees.Com4[i]], P2P: p2pChannels[committees.Com4[i]]}
	}
	return
}

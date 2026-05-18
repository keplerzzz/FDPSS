package random_generation

import (
	"fmt"

	"go-fdvss-fdpss/protocols/fdvss"
)

type RandomnessGenerationOutput struct {
	Index  uint64
	Shares []uint64
}

type RandomnessShare struct {
	RandomIndex int
	Index       uint64
	Value       uint64
}

func PerformCom4PiRG(
	pub *fdvss.PublicInput,
	prv *fdvss.PrivateInput,
	com3Msgs []fdvss.Com3Message,
	com1ToCom4 []fdvss.Com1ToCom4Message,
	vandermondeM [][]uint64,
) (*RandomnessGenerationOutput, error) {
	if pub == nil || prv == nil {
		return nil, fmt.Errorf("nil inputs")
	}
	if err := pub.Validate(); err != nil {
		return nil, err
	}
	com4Results, err := fdvss.PerformCom4(pub, prv, com3Msgs, com1ToCom4)
	if err != nil {
		return nil, err
	}
	return PerformRandomnessGenerationCom4(pub, prv, com4Results, vandermondeM)
}

func PerformRandomnessGenerationCom4(
	pub *fdvss.PublicInput,
	prv *fdvss.PrivateInput,
	com4Results []fdvss.Com4Result,
	vandermondeM [][]uint64,
) (*RandomnessGenerationOutput, error) {
	if pub == nil || prv == nil {
		return nil, fmt.Errorf("nil inputs")
	}
	if err := pub.Validate(); err != nil {
		return nil, err
	}
	if len(com4Results) == 0 {
		return nil, fmt.Errorf("no Com4 results")
	}
	if vandermondeM == nil {
		return nil, fmt.Errorf("nil Vandermonde matrix")
	}

	indices := pub.Committees.Indices(prv.ID)
	if indices.Com4 < 0 {
		return nil, fmt.Errorf("party %d is not in Com4", prv.ID)
	}

	n := pub.VSSParams.N
	t := pub.VSSParams.D
	expectedRandomCount := n - t

	if len(vandermondeM) != n {
		return nil, fmt.Errorf("Vandermonde matrix rows %d != n %d", len(vandermondeM), n)
	}
	if len(vandermondeM) > 0 && len(vandermondeM[0]) != expectedRandomCount {
		return nil, fmt.Errorf("Vandermonde matrix cols %d != n-t %d",
			len(vandermondeM[0]), expectedRandomCount)
	}

	wantSeq := uint64(indices.Com4 + 1)
	mShares, err := orderedCom4SharesForPiRG(pub, com4Results, wantSeq)
	if err != nil {
		return nil, err
	}

	matrixTranspose, err := MatrixTranspose(vandermondeM)
	if err != nil {
		return nil, fmt.Errorf("transpose Vandermonde: %w", err)
	}

	randomShares, err := MatrixVectorMultiply(pub.Field, matrixTranspose, mShares)
	if err != nil {
		return nil, fmt.Errorf("M^T * m: %w", err)
	}

	if len(randomShares) != expectedRandomCount {
		return nil, fmt.Errorf("unexpected random shares count %d (expected %d)",
			len(randomShares), expectedRandomCount)
	}

	return &RandomnessGenerationOutput{Index: wantSeq, Shares: randomShares}, nil
}

func orderedCom4SharesForPiRG(
	pub *fdvss.PublicInput,
	com4Results []fdvss.Com4Result,
	wantCom4Seq uint64,
) ([]uint64, error) {
	byDealer := make(map[int]fdvss.Com4Result, len(com4Results))
	for _, r := range com4Results {
		if !fdvss.Uint64Equal(r.Index, wantCom4Seq) {
			continue
		}
		byDealer[r.DealerID] = r
	}

	out := make([]uint64, 0, len(pub.Committees.Dealers))
	for _, dealerID := range pub.Committees.Dealers {
		r, ok := byDealer[dealerID]
		if !ok {
			return nil, fmt.Errorf("missing Com4 share for dealer %d (Com4 seq %d)", dealerID, wantCom4Seq)
		}
		out = append(out, r.Value)
	}

	if len(out) != len(pub.Committees.Dealers) {
		return nil, fmt.Errorf("internal: dealer list length mismatch")
	}
	if len(out) != pub.VSSParams.N {
		return nil, fmt.Errorf("Π_RG expects |Com0| = n dealers, got %d != n %d", len(out), pub.VSSParams.N)
	}

	return out, nil
}

func ConvertRandomnessOutputToShares(
	com4Index int,
	output *RandomnessGenerationOutput,
) []RandomnessShare {
	if output == nil {
		return nil
	}
	shares := make([]RandomnessShare, len(output.Shares))
	com4Seq := output.Index
	if com4Seq == 0 && com4Index >= 0 {
		com4Seq = uint64(com4Index + 1)
	}
	for i := 0; i < len(output.Shares); i++ {
		shares[i] = RandomnessShare{
			RandomIndex: i + 1,
			Index:       com4Seq,
			Value:       output.Shares[i],
		}
	}
	return shares
}

func ExtractCom4ResultsForParty(
	allCom4Results [][]fdvss.Com4Result,
	com4Index int,
) ([]fdvss.Com4Result, error) {
	if com4Index < 0 || com4Index >= len(allCom4Results) {
		return nil, fmt.Errorf("invalid Com4 index %d", com4Index)
	}
	return allCom4Results[com4Index], nil
}

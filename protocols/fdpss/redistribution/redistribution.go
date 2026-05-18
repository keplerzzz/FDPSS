package redistribution

import (
	"fmt"

	"go-fdvss-fdpss/protocols/fdpss/random_generation"
	"go-fdvss-fdpss/protocols/fdvss"
)

func RdMaskedOutgoing(f *fdvss.FieldParams, secretShare fdvss.Com4Result, receiverJ int, rShares []uint64) fdvss.Com4Result {
	m := secretShare.Value
	jp := f.Uint64FromInt(receiverJ)
	for l := 0; l < len(rShares); l++ {
		m = f.ModAdd(m, f.ModMul(rShares[l], jp))
		jp = f.ModMul(jp, f.Uint64FromInt(receiverJ))
	}
	return fdvss.Com4Result{
		DealerID: secretShare.DealerID,
		Index:    f.Uint64FromInt(receiverJ),
		Value:    m,
	}
}

func PrefixRGSharesForRD(
	pub *fdvss.PublicInput,
	prv *fdvss.PrivateInput,
	rgOut *random_generation.RandomnessGenerationOutput,
) ([]uint64, error) {
	if pub == nil || prv == nil || rgOut == nil {
		return nil, fmt.Errorf("nil input")
	}
	if err := pub.Validate(); err != nil {
		return nil, err
	}
	f := pub.Field
	indices := pub.Committees.Indices(prv.ID)
	if indices.Com4 < 0 {
		return nil, fmt.Errorf("party %d is not in Com4 (Com_k)", prv.ID)
	}
	wantSeq := f.Uint64FromInt(indices.Com4 + 1)
	if !fdvss.Uint64Equal(rgOut.Index, wantSeq) {
		return nil, fmt.Errorf("RG Index inconsistent with Com4 sequence for party %d", prv.ID)
	}
	d := pub.VSSParams.D
	if len(rgOut.Shares) < d {
		return nil, fmt.Errorf("RG has %d shares, need >= D=%d (Pi_RG length n-D)", len(rgOut.Shares), d)
	}
	return rgOut.Shares[:d], nil
}

func ComputeMaskedOutgoingsToAllReceivers(
	pub *fdvss.PublicInput,
	prv *fdvss.PrivateInput,
	secretShare fdvss.Com4Result,
	rgOut *random_generation.RandomnessGenerationOutput,
) ([]fdvss.Com4Result, error) {
	if pub == nil {
		return nil, fmt.Errorf("nil public input")
	}
	if err := pub.Validate(); err != nil {
		return nil, err
	}
	f := pub.Field
	if prv == nil {
		return nil, fmt.Errorf("nil private input")
	}
	if rgOut == nil {
		return nil, fmt.Errorf("nil RG output")
	}
	indices := pub.Committees.Indices(prv.ID)
	if indices.Com4 < 0 {
		return nil, fmt.Errorf("party %d is not in Com4 (Com_k)", prv.ID)
	}
	wantSeq := f.Uint64FromInt(indices.Com4 + 1)
	if !fdvss.Uint64Equal(secretShare.Index, wantSeq) {
		return nil, fmt.Errorf("secret Com4Result.Index inconsistent with Com4 sequence for party %d", prv.ID)
	}
	n := pub.VSSParams.N
	if pub.VSSParams.N <= pub.VSSParams.D {
		return nil, fmt.Errorf("invalid params: need N>D")
	}
	if !fdvss.Uint64Equal(rgOut.Index, secretShare.Index) {
		return nil, fmt.Errorf("RG Index inconsistent with secret Com4Result.Index")
	}
	rShares, err := PrefixRGSharesForRD(pub, prv, rgOut)
	if err != nil {
		return nil, err
	}
	out := make([]fdvss.Com4Result, n)
	for j := 1; j <= n; j++ {
		out[j-1] = RdMaskedOutgoing(f, secretShare, j, rShares)
	}
	return out, nil
}

func ReceiverReconstructShare(
	pub *fdvss.PublicInput,
	prv *fdvss.PrivateInput,
	dealerID int,
	receiverSeq int,
	mFromSenders []uint64,
) (*fdvss.Com4Result, error) {
	if pub == nil {
		return nil, fmt.Errorf("nil public input")
	}
	if err := pub.Validate(); err != nil {
		return nil, err
	}
	f := pub.Field
	if prv == nil {
		return nil, fmt.Errorf("nil private input")
	}
	n := pub.VSSParams.N
	d := pub.VSSParams.D
	if receiverSeq < 1 || receiverSeq > n {
		return nil, fmt.Errorf("receiverSeq %d out of range [1,%d]", receiverSeq, n)
	}
	if len(mFromSenders) != n {
		return nil, fmt.Errorf("mFromSenders length %d != N %d", len(mFromSenders), n)
	}
	witness := make([]fdvss.ShamirWitnessPoint, n)
	for i := 0; i < n; i++ {
		witness[i] = fdvss.ShamirWitnessPoint{X: f.Uint64FromInt(i + 1), Y: mFromSenders[i]}
	}
	val, err := fdvss.ReconstructPolynomialValueAtZero(f, witness, d)
	if err != nil {
		return nil, err
	}
	return &fdvss.Com4Result{
		DealerID: dealerID,
		Index:    f.Uint64FromInt(receiverSeq),
		Value:    val,
	}, nil
}

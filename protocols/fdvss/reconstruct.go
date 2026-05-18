package fdvss

import (
	"fmt"

	"go-fdvss-fdpss/msgpack"
)

type Com4ResultMessage struct {
	From    int
	Results []Com4Result
}

func BroadcastCom4Result(prv *PrivateInput, results []Com4Result) error {
	if prv.BC == nil {
		return fmt.Errorf("party %d missing broadcast channel", prv.ID)
	}
	msg := Com4ResultMessage{
		From:    prv.ID,
		Results: results,
	}
	payload := msgpack.Encode(&msg)
	prv.BC.Send(payload)
	return nil
}

func ReconstructSecret(
	pub *PublicInput,
	com4ResultMsgs []Com4ResultMessage,
) (map[int]uint64, error) {
	if pub == nil {
		return nil, fmt.Errorf("nil public input")
	}
	if err := pub.Validate(); err != nil {
		return nil, err
	}
	f := pub.Field
	if len(com4ResultMsgs) == 0 {
		return nil, fmt.Errorf("no Com4 result messages")
	}

	resultsByDealer := make(map[int][]Com4Result)
	for _, msg := range com4ResultMsgs {
		for _, result := range msg.Results {
			resultsByDealer[result.DealerID] = append(resultsByDealer[result.DealerID], result)
		}
	}

	reconstructed := make(map[int]uint64)
	for dealerID, results := range resultsByDealer {
		if len(results) == 0 {
			continue
		}

		points := make([]lagrangePoint, 0, len(results))
		for _, result := range results {
			points = append(points, lagrangePoint{
				x: result.Index,
				y: result.Value,
			})
		}

		if len(points) <= pub.VSSParams.D {
			return nil, fmt.Errorf("dealer %d: insufficient points for reconstruction (have %d, need %d)",
				dealerID, len(points), pub.VSSParams.D+1)
		}

		recoveredValue, err := reedSolomonDecode(f, points, pub.VSSParams.D)
		if err != nil {
			return nil, fmt.Errorf("dealer %d: Reed-Solomon decode failed: %w", dealerID, err)
		}

		if Uint64IsZero(recoveredValue) {
			fmt.Printf("Warning: dealer %d reconstructed value is 0, dealer may be disqualified\n", dealerID)
		}

		reconstructed[dealerID] = recoveredValue
	}

	return reconstructed, nil
}

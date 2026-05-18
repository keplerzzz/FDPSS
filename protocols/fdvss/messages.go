package fdvss

type DealerProjection struct {
	Index  int
	Matrix [][]uint64
}

type DealerMessage struct {
	DealerID       int
	RowProjections []DealerProjection
	ColProjections []DealerProjection
	FuturePayloads []DealerProjection
}

type DealerToCom1Message struct {
	DealerID       int
	RowProjections []DealerProjection
	ColProjections []DealerProjection
}

type Com1Entry struct {
	DealerID int
	Com1ID   int
	Row      DealerProjection
	Col      DealerProjection
}

type Com1ToCom2Message struct {
	DealerID int
	Com1ID   int
	Origin   int
	Target   int
	RowShare [][]uint64
	ColShare [][]uint64
}

type Com2Message struct {
	BroadcastData []Com2BroadcastData
}

type Com3Message struct {
	BroadcastData []Com3BroadcastData
}

type Com2PairBroadcast struct {
	Com1A     int
	Com1B     int
	LeftDiff  []uint64
	RightDiff []uint64
}

type Com2BroadcastData struct {
	DealerID int
	Pairs    []Com2PairBroadcast
}

type Com3BroadcastData struct {
	DealerID       int
	Unhappy        []int
	Disqualified   bool
	DealerShares   []DealerColumnShare
	Com1PeerShares []Com1ColumnShare
}

type DealerColumnShare struct {
	DealerID int
	Com1Seq  int
	Com3Seq  int
	Matrix   [][]uint64
}

type Com1ColumnShare struct {
	DealerID  int
	Com1Seq   int
	PeerIndex int
	Com3Seq   int
	Matrix    []uint64
}

type Com1ToCom3Message struct {
	Share Com1ColumnShare
}

type Com1ToCom4Message struct {
	DealerID int
	Com1ID   int
	Target   int
	Coeffs   []uint64
}

type DealerToCom3Message struct {
	DealerID int
	Shares   []DealerColumnShare
}

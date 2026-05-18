package communication

type BroadcastMessage struct {
	_struct  struct{} `codec:",omitempty,omitemptyarray"`
	Payload  []byte   `codec:"payload"`
	SenderID int      `codec:"snd_id"`
}

type RoundMessages struct {
	_struct  struct{}           `codec:",omitempty,omitemptyarray"`
	Messages []BroadcastMessage `codec:"msgs"`
	Round    int                `codec:"rnd"`
}

type BroadcastChannel interface {
	Send(msg []byte)
	ReceiveRound() (int, []BroadcastMessage)
}

type PointToPointMessage struct {
	_struct  struct{} `codec:",omitempty,omitemptyarray"`
	Payload  []byte   `codec:"payload"`
	SenderID int      `codec:"snd_id"`
	TargetID int      `codec:"tgt_id"`
}

type PointToPointChannel interface {
	SendTo(targetID int, msg []byte)
	ReceiveFrom(senderID int) ([]byte, error)
	ReceiveAll() (map[int][]byte, error)
}

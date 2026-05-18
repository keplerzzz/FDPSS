package fake

import (
	"fmt"
	"sync"
	"time"

	"go-fdvss-fdpss/communication"
)

// Orchestrator simulates a secure broadcast channel
// used for communication between parties
type Orchestrator struct {
	Channels     map[int]PartyBroadcastChannel
	P2PChannels  map[int]PartyPointToPointChannel
	RoundMsgs    map[int]communication.BroadcastMessage
	P2PMsgs      map[int]map[int][]byte // P2PMsgs[targetID][senderID] = payload
	MessageSizes map[int]int
	Round        int
	mu           sync.RWMutex // 保护 P2PMsgs 的并发访问
}

// NewOrchestrator creates a new orchestrator
func NewOrchestrator() Orchestrator {
	return Orchestrator{
		Channels:     make(map[int]PartyBroadcastChannel),
		P2PChannels:  make(map[int]PartyPointToPointChannel),
		RoundMsgs:    make(map[int]communication.BroadcastMessage),
		P2PMsgs:      make(map[int]map[int][]byte),
		MessageSizes: make(map[int]int),
		Round:        0,
	}
}

// AddChannel connects a party's channel to the orchestrator to participate in the protocol
func (o Orchestrator) AddChannel(pbc PartyBroadcastChannel) {
	o.Channels[pbc.ID] = pbc
}

// BroadcastChannel gets the party specified by the id
func (o Orchestrator) BroadcastChannel(id int) (*PartyBroadcastChannel, error) {
	pbc, ok := o.Channels[id]
	if !ok {
		return nil, fmt.Errorf("channel not found for id: %d", id)
	}
	return &pbc, nil
}

// ReceiveMessages is used by the orchestrator to collect messages from all parties
// in a given round. It waits for messages with a timeout to avoid blocking forever.
func (o *Orchestrator) ReceiveMessages() error {

	// Code for benchmarking
	//fmt.Printf("receive time: %v \n", time.Now())

	// Clear previous round messages
	o.RoundMsgs = make(map[int]communication.BroadcastMessage)

	// Simultaneously listen to channels opened with the parties
	agg := make(chan communication.BroadcastMessage, len(o.Channels))
	var wg sync.WaitGroup
	for _, pbc := range o.Channels {
		wg.Add(1)
		go func(c chan communication.BroadcastMessage, wg *sync.WaitGroup) {
			defer wg.Done()
			// Try to receive message, with timeout to avoid blocking forever
			// First try non-blocking receive, then wait with timeout
			select {
			case msg := <-c:
				agg <- msg
			default:
				// No message immediately available, wait with timeout
				select {
				case msg := <-c:
					agg <- msg
				case <-time.After(1000 * time.Millisecond):
					// Timeout: no message from this channel, skip it
				}
			}
		}(pbc.SendChannel, &wg)
	}

	wg.Wait()
	close(agg)

	// Iterate through all the received messages
	for bcastMsg := range agg {
		o.RoundMsgs[bcastMsg.SenderID] = bcastMsg
		o.MessageSizes[bcastMsg.SenderID] += len(bcastMsg.Payload)
	}

	return nil
}

func (o Orchestrator) WaitMessageChannel(channel int) {
	msg := <-o.Channels[channel].SendChannel // wait
	o.Channels[channel].SendChannel <- msg   // put back the message
}

func (o Orchestrator) collectRoundMessages() communication.RoundMessages {

	var msgs []communication.BroadcastMessage
	for i := 0; i < len(o.Channels); i++ {
		if msg, ok := o.RoundMsgs[i]; ok && msg.SenderID == i {
			msgs = append(msgs, msg)
		}
	}
	roundMsgs := communication.RoundMessages{
		Messages: msgs,
		Round:    o.Round,
	}
	return roundMsgs
}

func (o Orchestrator) SendMessageChannels(channels []int) error {
	roundMsgs := o.collectRoundMessages()

	for _, i := range channels {
		o.Channels[i].ReceiveChannel <- roundMsgs
	}
	return nil
}

func (o Orchestrator) Broadcast() error {
	roundMsgs := o.collectRoundMessages()

	for _, bc := range o.Channels {
		bc.ReceiveChannel <- roundMsgs
	}
	return nil
}

type PartyBroadcastChannel struct {
	ID             int
	SendChannel    chan communication.BroadcastMessage
	ReceiveChannel chan communication.RoundMessages
}

func NewPartyBroadcastChannel(id int) PartyBroadcastChannel {
	return PartyBroadcastChannel{
		ID:             id,
		SendChannel:    make(chan communication.BroadcastMessage, 1),
		ReceiveChannel: make(chan communication.RoundMessages, 1),
	}
}

func (pbc PartyBroadcastChannel) Send(msg []byte) {
	bcastMsg := communication.BroadcastMessage{
		Payload:  msg,
		SenderID: pbc.ID,
	}

	pbc.SendChannel <- bcastMsg
}

func (pbc PartyBroadcastChannel) ReceiveRound() (int, []communication.BroadcastMessage) {
	roundMsgs := <-pbc.ReceiveChannel
	return roundMsgs.Round, []communication.BroadcastMessage(roundMsgs.Messages)
}

func (o *Orchestrator) AddP2PChannel(ppc PartyPointToPointChannel) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.P2PChannels[ppc.ID] = ppc
	if o.P2PMsgs[ppc.ID] == nil {
		o.P2PMsgs[ppc.ID] = make(map[int][]byte)
	}
}

func (o *Orchestrator) PointToPointChannel(id int) (*PartyPointToPointChannel, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	ppc, ok := o.P2PChannels[id]
	if !ok {
		return nil, fmt.Errorf("P2P channel not found for id: %d", id)
	}
	return &ppc, nil
}

func (o *Orchestrator) DeliverP2PMessages() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	for targetID, senderMsgs := range o.P2PMsgs {
		ppc, ok := o.P2PChannels[targetID]
		if !ok {
			continue
		}

		for senderID, payload := range senderMsgs {
			msg := communication.PointToPointMessage{
				Payload:  payload,
				SenderID: senderID,
				TargetID: targetID,
			}
			select {
			case ppc.ReceiveChannel <- msg:
			default:

			}
		}

		o.P2PMsgs[targetID] = make(map[int][]byte)
	}
	return nil
}

type PartyPointToPointChannel struct {
	ID             int
	SendChannel    chan communication.PointToPointMessage
	ReceiveChannel chan communication.PointToPointMessage
	orchestrator   *Orchestrator
}

func NewPartyPointToPointChannel(id int, orchestrator *Orchestrator) PartyPointToPointChannel {
	ppc := PartyPointToPointChannel{
		ID:             id,
		SendChannel:    make(chan communication.PointToPointMessage, 10000),
		ReceiveChannel: make(chan communication.PointToPointMessage, 10000),
		orchestrator:   orchestrator,
	}

	go func() {
		for msg := range ppc.SendChannel {
			orchestrator.mu.Lock()
			if orchestrator.P2PMsgs[msg.TargetID] == nil {
				orchestrator.P2PMsgs[msg.TargetID] = make(map[int][]byte)
			}
			orchestrator.P2PMsgs[msg.TargetID][msg.SenderID] = msg.Payload
			orchestrator.mu.Unlock()
		}
	}()
	return ppc
}

func (ppc PartyPointToPointChannel) SendTo(targetID int, msg []byte) {
	p2pMsg := communication.PointToPointMessage{
		Payload:  msg,
		SenderID: ppc.ID,
		TargetID: targetID,
	}
	ppc.SendChannel <- p2pMsg
}

func (ppc PartyPointToPointChannel) ReceiveFrom(senderID int) ([]byte, error) {

	ppc.orchestrator.mu.RLock()
	if ppc.orchestrator.P2PMsgs[ppc.ID] != nil {
		if payload, ok := ppc.orchestrator.P2PMsgs[ppc.ID][senderID]; ok {
			ppc.orchestrator.mu.RUnlock()
			ppc.orchestrator.mu.Lock()
			delete(ppc.orchestrator.P2PMsgs[ppc.ID], senderID)
			ppc.orchestrator.mu.Unlock()
			return payload, nil
		}
	}
	ppc.orchestrator.mu.RUnlock()

	select {
	case msg := <-ppc.ReceiveChannel:
		if msg.SenderID == senderID {
			return msg.Payload, nil
		}
		select {
		case ppc.ReceiveChannel <- msg:
		default:
		}
		return nil, fmt.Errorf("message from unexpected sender %d (expected %d)", msg.SenderID, senderID)
	default:
		return nil, fmt.Errorf("no message from sender %d", senderID)
	}
}

func (ppc PartyPointToPointChannel) ReceiveAll() (map[int][]byte, error) {
	result := make(map[int][]byte)

	ppc.orchestrator.mu.Lock()
	if ppc.orchestrator.P2PMsgs[ppc.ID] != nil {
		for senderID, payload := range ppc.orchestrator.P2PMsgs[ppc.ID] {
			result[senderID] = payload
		}

		ppc.orchestrator.P2PMsgs[ppc.ID] = make(map[int][]byte)
	}
	ppc.orchestrator.mu.Unlock()

	for {
		select {
		case msg := <-ppc.ReceiveChannel:
			result[msg.SenderID] = msg.Payload
		default:

			return result, nil
		}
	}
}

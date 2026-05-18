package fdvss

import (
	"fmt"
	"runtime"
	"time"

	"go-fdvss-fdpss/communication"
	"go-fdvss-fdpss/msgpack"
)

func sendMessage(bc communication.BroadcastChannel, msg interface{}) error {
	payload := msgpack.Encode(msg)
	bc.Send(payload)
	return nil
}

func receivePayloads(bc communication.BroadcastChannel) (map[int][]byte, error) {
	round, msgs := bc.ReceiveRound()
	if round < 0 {
		return nil, fmt.Errorf("invalid round received")
	}
	payloads := make(map[int][]byte, len(msgs))
	for _, m := range msgs {
		payloads[m.SenderID] = m.Payload
	}
	return payloads, nil
}

func requireBroadcastChannel(prv *PrivateInput) error {
	if prv.BC == nil {
		return fmt.Errorf("party %d missing broadcast channel", prv.ID)
	}
	return nil
}

func requireP2PChannel(prv *PrivateInput) error {
	if prv.P2P == nil {
		return fmt.Errorf("party %d missing point-to-point channel", prv.ID)
	}
	return nil
}

func sendP2PMessage(p2p communication.PointToPointChannel, targetID int, msg interface{}) error {
	payload := msgpack.Encode(msg)
	p2p.SendTo(targetID, payload)
	return nil
}

func SendDealerToCom1(pub *PublicInput, prv *PrivateInput, msg *DealerMessage, targetCom1ID int) error {
	if err := requireP2PChannel(prv); err != nil {
		return err
	}
	com1Seq := 0
	for idx, id := range pub.Committees.Com1 {
		if id == targetCom1ID {
			com1Seq = idx + 1
			break
		}
	}
	if com1Seq == 0 {
		return fmt.Errorf("Com1 ID %d not found in committees", targetCom1ID)
	}

	var targetProjection *DealerProjection
	var targetColProjection *DealerProjection

	for i := range msg.RowProjections {
		if msg.RowProjections[i].Index == com1Seq {
			targetProjection = &msg.RowProjections[i]
			break
		}
	}
	for i := range msg.ColProjections {
		if msg.ColProjections[i].Index == com1Seq {
			targetColProjection = &msg.ColProjections[i]
			break
		}
	}

	if targetProjection == nil || targetColProjection == nil {
		return fmt.Errorf("dealer %d missing projection for Com1 %d", prv.ID, targetCom1ID)
	}

	com1Msg := &Com1Entry{
		DealerID: msg.DealerID,
		Com1ID:   com1Seq,
		Row:      *targetProjection,
		Col:      *targetColProjection,
	}

	return sendP2PMessage(prv.P2P, targetCom1ID, com1Msg)
}

func SendDealerToCom1Batch(pub *PublicInput, prv *PrivateInput, msg *DealerMessage, com1IDs []int) error {
	if err := requireP2PChannel(prv); err != nil {
		return err
	}
	for _, com1ID := range com1IDs {
		if err := SendDealerToCom1(pub, prv, msg, com1ID); err != nil {
			return fmt.Errorf("failed to send to Com1 %d: %w", com1ID, err)
		}
	}
	return nil
}

func ReceiveDealerFromCom1(p2p communication.PointToPointChannel, senderID int) (*Com1Entry, error) {
	data, err := p2p.ReceiveFrom(senderID)
	if err != nil {
		return nil, fmt.Errorf("failed to receive from Dealer %d: %w", senderID, err)
	}
	var entry Com1Entry
	if err := msgpack.Decode(data, &entry); err != nil {
		return nil, fmt.Errorf("decoding Com1Entry from dealer %d failed: %w", senderID, err)
	}
	return &entry, nil
}

func ReceiveDealerFromCom1Reliable(p2p communication.PointToPointChannel, dealerID int) (*Com1Entry, error) {
	deadline := time.Now().Add(500 * time.Millisecond)
	var lastErr error
	for time.Now().Before(deadline) {
		entry, err := ReceiveDealerFromCom1(p2p, dealerID)
		if err == nil {
			return entry, nil
		}
		lastErr = err
		runtime.Gosched()
	}
	return nil, lastErr
}

func ReceiveDealerFromCom1All(p2p communication.PointToPointChannel, dealerIDs []int) ([]Com1Entry, error) {
	allMsgs, err := p2p.ReceiveAll()
	if err != nil {
		return nil, fmt.Errorf("failed to receive all messages: %w", err)
	}
	entries := make([]Com1Entry, 0)
	for _, dealerID := range dealerIDs {
		data, ok := allMsgs[dealerID]
		if !ok {
			continue
		}
		var entry Com1Entry
		if err := msgpack.Decode(data, &entry); err != nil {
			return nil, fmt.Errorf("decoding Com1Entry from dealer %d failed: %w", dealerID, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func SendCom1ToCom2(prv *PrivateInput, msg Com1ToCom2Message) error {
	if err := requireP2PChannel(prv); err != nil {
		return err
	}
	return sendP2PMessage(prv.P2P, msg.Target, &msg)
}

func SendCom1ToCom2Batch(prv *PrivateInput, msgs []Com1ToCom2Message) error {
	if err := requireP2PChannel(prv); err != nil {
		return err
	}
	for _, msg := range msgs {
		if err := sendP2PMessage(prv.P2P, msg.Target, &msg); err != nil {
			return fmt.Errorf("failed to send message to Com2 %d: %w", msg.Target, err)
		}
	}
	return nil
}

func ReceiveCom1ToCom2(p2p communication.PointToPointChannel, senderID int) (*Com1ToCom2Message, error) {
	data, err := p2p.ReceiveFrom(senderID)
	if err != nil {
		return nil, fmt.Errorf("failed to receive from Com1 %d: %w", senderID, err)
	}
	var msg Com1ToCom2Message
	if err := msgpack.Decode(data, &msg); err != nil {
		return nil, fmt.Errorf("decoding Com1ToCom2 message from party %d failed: %w", senderID, err)
	}
	return &msg, nil
}

func ReceiveCom1ToCom2All(p2p communication.PointToPointChannel, com1Parties []int) ([]Com1ToCom2Message, error) {
	allMsgs, err := p2p.ReceiveAll()
	if err != nil {
		return nil, fmt.Errorf("failed to receive all messages: %w", err)
	}
	messages := make([]Com1ToCom2Message, 0, len(com1Parties))
	for _, senderID := range com1Parties {
		data, ok := allMsgs[senderID]
		if !ok {
			continue
		}
		var msg Com1ToCom2Message
		if err := msgpack.Decode(data, &msg); err != nil {
			return nil, fmt.Errorf("decoding Com1ToCom2 message from party %d failed: %w", senderID, err)
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func BroadcastCom2(prv *PrivateInput, msg *Com2Message) error {
	if err := requireBroadcastChannel(prv); err != nil {
		return err
	}
	return sendMessage(prv.BC, msg)
}

func ReceiveCom2Messages(bc communication.BroadcastChannel, parties []int) ([]Com2Message, error) {
	payloads, err := receivePayloads(bc)
	if err != nil {
		return nil, err
	}
	messages := make([]Com2Message, len(parties))
	for i, party := range parties {
		data, ok := payloads[party]
		if !ok {
			return nil, fmt.Errorf("missing com2 message from party %d", party)
		}
		if err := msgpack.Decode(data, &messages[i]); err != nil {
			return nil, fmt.Errorf("decoding com2 message from party %d failed: %w", party, err)
		}
	}
	return messages, nil
}

func SendCom1ToCom3(prv *PrivateInput, msg Com1ToCom3Message, targetCom3ID int) error {
	if err := requireP2PChannel(prv); err != nil {
		return err
	}
	return sendP2PMessage(prv.P2P, targetCom3ID, &msg)
}

func SendCom1ToCom3Batch(prv *PrivateInput, msgs []Com1ToCom3Message, com3IDs []int) error {
	if err := requireP2PChannel(prv); err != nil {
		return err
	}

	msgsByCom3Seq := make(map[int][]Com1ToCom3Message)
	for _, msg := range msgs {
		com3Seq := msg.Share.Com3Seq
		if com3Seq <= 0 || com3Seq > len(com3IDs) {
			return fmt.Errorf("invalid Com3 sequence number %d (must be between 1 and %d)", com3Seq, len(com3IDs))
		}
		msgsByCom3Seq[com3Seq] = append(msgsByCom3Seq[com3Seq], msg)
	}

	for i, com3ID := range com3IDs {
		com3Seq := i + 1
		com3Msgs, ok := msgsByCom3Seq[com3Seq]
		if !ok {
			continue
		}
		for _, msg := range com3Msgs {
			if err := sendP2PMessage(prv.P2P, com3ID, &msg); err != nil {
				return fmt.Errorf("failed to send message to Com3 %d (seq %d): %w", com3ID, com3Seq, err)
			}
		}
	}
	return nil
}

func ReceiveCom1ToCom3(p2p communication.PointToPointChannel, senderID int) (*Com1ToCom3Message, error) {
	data, err := p2p.ReceiveFrom(senderID)
	if err != nil {
		return nil, fmt.Errorf("failed to receive from Com1 %d: %w", senderID, err)
	}
	var msg Com1ToCom3Message
	if err := msgpack.Decode(data, &msg); err != nil {
		return nil, fmt.Errorf("decoding Com1ToCom3 message from party %d failed: %w", senderID, err)
	}
	return &msg, nil
}

func ReceiveCom1ToCom3All(p2p communication.PointToPointChannel, com1Parties []int) ([]Com1ToCom3Message, error) {
	allMsgs, err := p2p.ReceiveAll()
	if err != nil {
		return nil, fmt.Errorf("failed to receive all messages: %w", err)
	}
	messages := make([]Com1ToCom3Message, 0)
	for _, senderID := range com1Parties {
		data, ok := allMsgs[senderID]
		if !ok {
			continue
		}
		var msg Com1ToCom3Message
		if err := msgpack.Decode(data, &msg); err != nil {
			return nil, fmt.Errorf("decoding Com1ToCom3 message from party %d failed: %w", senderID, err)
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func SendDealerToCom3(prv *PrivateInput, share DealerColumnShare, targetCom3ID int) error {
	if err := requireP2PChannel(prv); err != nil {
		return err
	}
	return sendP2PMessage(prv.P2P, targetCom3ID, &share)
}

func SendDealerToCom3Batch(prv *PrivateInput, msg *DealerToCom3Message, com3IDs []int) error {
	if err := requireP2PChannel(prv); err != nil {
		return err
	}

	for i, com3ID := range com3IDs {
		com3Seq := i + 1

		com3Shares := make([]DealerColumnShare, 0)
		for _, s := range msg.Shares {
			if s.Com3Seq == com3Seq {
				com3Shares = append(com3Shares, s)
			}
		}
		if len(com3Shares) > 0 {
			com3Msg := DealerToCom3Message{
				DealerID: msg.DealerID,
				Shares:   com3Shares,
			}
			if err := sendP2PMessage(prv.P2P, com3ID, &com3Msg); err != nil {
				return fmt.Errorf("failed to send to Com3 %d: %w", com3ID, err)
			}
		}
	}
	return nil
}

func ReceiveDealerToCom3(p2p communication.PointToPointChannel, senderID int) (*DealerToCom3Message, error) {
	data, err := p2p.ReceiveFrom(senderID)
	if err != nil {
		return nil, fmt.Errorf("failed to receive from Dealer %d: %w", senderID, err)
	}
	var msg DealerToCom3Message
	if err := msgpack.Decode(data, &msg); err != nil {
		return nil, fmt.Errorf("decoding DealerToCom3Message from dealer %d failed: %w", senderID, err)
	}
	return &msg, nil
}

func ReceiveDealerToCom3All(p2p communication.PointToPointChannel, dealerIDs []int) ([]DealerToCom3Message, error) {
	allMsgs, err := p2p.ReceiveAll()
	if err != nil {
		return nil, fmt.Errorf("failed to receive all messages: %w", err)
	}
	messages := make([]DealerToCom3Message, 0)
	for _, dealerID := range dealerIDs {
		data, ok := allMsgs[dealerID]
		if !ok {
			continue
		}
		var msg DealerToCom3Message
		if err := msgpack.Decode(data, &msg); err != nil {
			return nil, fmt.Errorf("decoding DealerToCom3Message from dealer %d failed: %w", dealerID, err)
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func BroadcastCom3(
	pub *PublicInput,
	prv *PrivateInput,
	com2Msgs []Com2Message,
	dealerShares []DealerToCom3Message,
	com1ToCom3 []Com1ToCom3Message,
) (*Com3Message, error) {
	if err := requireBroadcastChannel(prv); err != nil {
		return nil, err
	}
	msg, err := PerformCom3(pub, prv, com2Msgs, dealerShares, com1ToCom3)
	if err != nil {
		return nil, err
	}
	if err := sendMessage(prv.BC, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func ReceiveCom3Messages(bc communication.BroadcastChannel, parties []int) ([]Com3Message, error) {
	payloads, err := receivePayloads(bc)
	if err != nil {
		return nil, err
	}
	messages := make([]Com3Message, len(parties))
	for i, party := range parties {
		data, ok := payloads[party]
		if !ok {
			return nil, fmt.Errorf("missing com3 message from party %d", party)
		}
		if err := msgpack.Decode(data, &messages[i]); err != nil {
			return nil, fmt.Errorf("decoding com3 message from party %d failed: %w", party, err)
		}
	}
	return messages, nil
}

func SendCom1ToCom4(prv *PrivateInput, msg Com1ToCom4Message, targetRecipientID int) error {
	if err := requireP2PChannel(prv); err != nil {
		return err
	}

	return sendP2PMessage(prv.P2P, targetRecipientID, &msg)
}

func SendCom1ToCom4Batch(prv *PrivateInput, msgs []Com1ToCom4Message, recipientIDs []int) error {
	if err := requireP2PChannel(prv); err != nil {
		return err
	}

	msgByTarget := make(map[int]Com1ToCom4Message)
	for _, msg := range msgs {
		msgByTarget[msg.Target] = msg
	}
	for i, recipientID := range recipientIDs {
		targetSeq := i + 1
		msg, ok := msgByTarget[targetSeq]
		if !ok {
			continue
		}
		if err := sendP2PMessage(prv.P2P, recipientID, &msg); err != nil {
			return fmt.Errorf("failed to send message to Recipient %d: %w", recipientID, err)
		}
	}
	return nil
}

func ReceiveCom1ToCom4(p2p communication.PointToPointChannel, senderID int) (*Com1ToCom4Message, error) {
	data, err := p2p.ReceiveFrom(senderID)
	if err != nil {
		return nil, fmt.Errorf("failed to receive from Com1 %d: %w", senderID, err)
	}
	var msg Com1ToCom4Message
	if err := msgpack.Decode(data, &msg); err != nil {
		return nil, fmt.Errorf("decoding Com1ToCom4 message from party %d failed: %w", senderID, err)
	}
	return &msg, nil
}

func ReceiveCom1ToCom4All(p2p communication.PointToPointChannel, com1Parties []int) ([]Com1ToCom4Message, error) {
	allMsgs, err := p2p.ReceiveAll()
	if err != nil {
		return nil, fmt.Errorf("failed to receive all messages: %w", err)
	}
	messages := make([]Com1ToCom4Message, 0)
	for _, senderID := range com1Parties {
		data, ok := allMsgs[senderID]
		if !ok {
			continue
		}
		var msg Com1ToCom4Message
		if err := msgpack.Decode(data, &msg); err != nil {
			return nil, fmt.Errorf("decoding Com1ToCom4 message from party %d failed: %w", senderID, err)
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func ReceiveCom4Results(bc communication.BroadcastChannel, parties []int) ([]Com4ResultMessage, error) {
	round, msgs := bc.ReceiveRound()
	if round < 0 {
		return nil, fmt.Errorf("invalid round received")
	}
	payloads := make(map[int][]byte, len(msgs))
	for _, m := range msgs {
		payloads[m.SenderID] = m.Payload
	}

	messages := make([]Com4ResultMessage, len(parties))
	for i, party := range parties {
		data, ok := payloads[party]
		if !ok {
			return nil, fmt.Errorf("missing Com4 result message from party %d", party)
		}
		if err := msgpack.Decode(data, &messages[i]); err != nil {
			return nil, fmt.Errorf("decoding Com4 result message from party %d failed: %w", party, err)
		}
	}
	return messages, nil
}

package p2p

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	ma "github.com/multiformats/go-multiaddr"
	"google.golang.org/protobuf/encoding/protowire"
)

const (
	PubSubDiscoveryTopic    = "io.github.santaklouse.p2p-netcat.peer-discovery.v1"
	PubSubDiscoveryInterval = 10 * time.Second
	maxPubSubRecordBytes    = 64 * 1024
	maxPubSubAddresses      = 64
)

type pubSubPeerRecord struct {
	PublicKey []byte
	Addresses []ma.Multiaddr
}

func (n *Node) startPubSub(
	ctx context.Context,
	directPeers []peer.AddrInfo,
	discover bool,
	peerExchange bool,
	interval time.Duration,
) error {
	options := []pubsub.Option{
		pubsub.WithMessageSigning(true),
		pubsub.WithStrictSignatureVerification(true),
		pubsub.WithFloodPublish(true),
		pubsub.WithPeerExchange(peerExchange),
	}
	if len(directPeers) > 0 {
		options = append(options, pubsub.WithDirectPeers(directPeers))
	}
	service, err := pubsub.NewGossipSub(ctx, n.Host, options...)
	if err != nil {
		return err
	}
	n.PubSub = service
	if !discover {
		return nil
	}
	topic, err := service.Join(PubSubDiscoveryTopic)
	if err != nil {
		return err
	}
	subscription, err := topic.Subscribe()
	if err != nil {
		_ = topic.Close()
		return err
	}
	n.pubsubTopic = topic
	n.pubsubSubscription = subscription
	if interval <= 0 {
		interval = PubSubDiscoveryInterval
	}
	go n.consumePubSubDiscovery(ctx, subscription)
	go n.publishPubSubDiscovery(ctx, topic, interval)
	return nil
}

func (n *Node) publishPubSubDiscovery(ctx context.Context, topic *pubsub.Topic, interval time.Duration) {
	publish := func() {
		if len(topic.ListPeers()) == 0 {
			return
		}
		record, err := encodePubSubPeerRecord(n.Host)
		if err == nil {
			err = topic.Publish(ctx, record)
		}
		if err != nil && ctx.Err() == nil && n.verbose {
			log.Printf("[p2p-nc] GossipSub discovery publish: %v", err)
		}
	}
	publish()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			publish()
		}
	}
}

func (n *Node) consumePubSubDiscovery(ctx context.Context, subscription *pubsub.Subscription) {
	for {
		message, err := subscription.Next(ctx)
		if err != nil {
			if ctx.Err() == nil && n.verbose {
				log.Printf("[p2p-nc] GossipSub discovery receive: %v", err)
			}
			return
		}
		record, err := decodePubSubPeerRecord(message.Data)
		if err != nil {
			if n.verbose {
				log.Printf("[p2p-nc] GossipSub discovery record: %v", err)
			}
			continue
		}
		peerID, err := peer.IDFromPublicKey(record.publicKey)
		if err != nil || peerID == n.Host.ID() || message.GetFrom() != peerID {
			continue
		}
		n.Host.Peerstore().AddAddrs(peerID, record.addresses, peerstore.TempAddrTTL)
	}
}

type decodedPubSubPeerRecord struct {
	publicKey crypto.PubKey
	addresses []ma.Multiaddr
}

func encodePubSubPeerRecord(h interface {
	ID() peer.ID
	Addrs() []ma.Multiaddr
	Peerstore() peerstore.Peerstore
}) ([]byte, error) {
	publicKey := h.Peerstore().PubKey(h.ID())
	if publicKey == nil {
		return nil, errors.New("local PeerId does not contain a public key")
	}
	publicBytes, err := crypto.MarshalPublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	output := protowire.AppendTag(nil, 1, protowire.BytesType)
	output = protowire.AppendBytes(output, publicBytes)
	for index, address := range h.Addrs() {
		if index >= maxPubSubAddresses {
			break
		}
		output = protowire.AppendTag(output, 2, protowire.BytesType)
		output = protowire.AppendBytes(output, address.Bytes())
	}
	if len(output) > maxPubSubRecordBytes {
		return nil, fmt.Errorf("GossipSub peer record exceeds %d bytes", maxPubSubRecordBytes)
	}
	return output, nil
}

func decodePubSubPeerRecord(input []byte) (decodedPubSubPeerRecord, error) {
	var result decodedPubSubPeerRecord
	if len(input) == 0 || len(input) > maxPubSubRecordBytes {
		return result, errors.New("invalid GossipSub peer record size")
	}
	var publicBytes []byte
	for len(input) > 0 {
		number, wireType, count := protowire.ConsumeTag(input)
		if count < 0 {
			return result, protowire.ParseError(count)
		}
		input = input[count:]
		if wireType != protowire.BytesType {
			return result, errors.New("GossipSub peer record contains a field with the wrong wire type")
		}
		value, valueCount := protowire.ConsumeBytes(input)
		if valueCount < 0 {
			return result, protowire.ParseError(valueCount)
		}
		input = input[valueCount:]
		switch number {
		case 1:
			publicBytes = append([]byte(nil), value...)
		case 2:
			if len(result.addresses) >= maxPubSubAddresses {
				return result, errors.New("GossipSub peer record contains too many addresses")
			}
			address, err := ma.NewMultiaddrBytes(value)
			if err != nil {
				return result, fmt.Errorf("GossipSub multiaddr: %w", err)
			}
			result.addresses = append(result.addresses, address)
		}
	}
	if len(publicBytes) == 0 {
		return result, errors.New("GossipSub peer record does not contain a public key")
	}
	publicKey, err := crypto.UnmarshalPublicKey(publicBytes)
	if err != nil {
		return result, fmt.Errorf("GossipSub public key: %w", err)
	}
	result.publicKey = publicKey
	return result, nil
}

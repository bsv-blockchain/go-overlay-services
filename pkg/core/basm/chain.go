package basm

import (
	"fmt"
	"math"
)

// Chain calculates a contiguous TAC from topic genesis. Its fields are private
// so callers cannot accidentally substitute an unverified peer checkpoint for
// the genesis seed. It is an immutable value: Append returns a replacement.
// This is an in-memory calculation, not durable or canonical-chain evidence.
type Chain struct {
	topic   string
	genesis uint32
	limits  Limits
	height  uint32
	tac     Hash
	hasTip  bool
}

// NewChain starts topic's TAC with zero at genesisHeight-1, including genesis
// height zero without unsigned underflow. limits bounds subsequent anchors.
func NewChain(topic string, genesisHeight uint32, limits Limits) (Chain, error) {
	if err := limits.validate(); err != nil {
		return Chain{}, err
	}
	if err := validateTopic(topic, limits.MaxTopicBytes); err != nil {
		return Chain{}, err
	}
	return Chain{topic: topic, genesis: genesisHeight, limits: limits}, nil
}

// Append returns a new chain including anchor, or the original value on error.
// The first anchor must be at genesis; subsequent anchors must have the same
// topic and the next height, including every empty-admission height. It checks
// shape and continuity, not block canonicality or root/list/proof authenticity.
func (c Chain) Append(anchor TopicBlockAnchor) (Chain, error) {
	if err := anchor.Validate(c.limits); err != nil {
		return c, err
	}
	if anchor.Topic != c.topic {
		return c, fmt.Errorf("%w: anchor topic differs from TAC topic", ErrNoncontiguous)
	}
	expected := c.genesis
	if c.hasTip {
		if c.height == math.MaxUint32 {
			return c, fmt.Errorf("%w: TAC height exhausted uint32 range", ErrNoncontiguous)
		}
		expected = c.height + 1
	}
	if anchor.BlockHeight != expected {
		return c, fmt.Errorf("%w: expected height %d, got %d", ErrNoncontiguous, expected, anchor.BlockHeight)
	}
	c.tac = HashTACStep(c.tac, anchor.BlockHash, anchor.BASMRoot)
	c.height = anchor.BlockHeight
	c.hasTip = true
	return c, nil
}

// Tip returns the calculated height and TAC, with ok=false before the first
// append. Different-height tips cannot directly establish history equality.
func (c Chain) Tip() (height uint32, tac Hash, ok bool) {
	return c.height, c.tac, c.hasTip
}

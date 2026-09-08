package engine

import (
	"context"
	"encoding/hex"
	"math/bits"
	"slices"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/transaction"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/basm"
)

// ProvideCompoundMerklePath serves a bounded union of direct BUMPs after checking
// every requested txid's height, original admission position, and canonical
// header root. It does not rerun topic admission or claim recovery agreement.
func (s *BASMReadService) ProvideCompoundMerklePath(ctx context.Context, topic string, height uint32, txids []basm.Hash) (basm.CompoundMerklePath, error) {
	if err := s.validateTxids(txids, true); err != nil {
		return basm.CompoundMerklePath{}, err
	}
	return readBASM(ctx, s, topic, true, func(ctx context.Context, v BASMReadView) (basm.CompoundMerklePath, []BASMCanonicalHeader, error) {
		_, admitted, header, err := s.readBlock(ctx, v, topic, height)
		if err != nil {
			return basm.CompoundMerklePath{}, nil, err
		}
		positions := make(map[basm.Hash]uint64, len(admitted))
		for _, ref := range admitted {
			positions[ref.TxID] = ref.BlockIndex
		}
		depth := bits.Len64(header.TransactionCount - 1)
		if depth == 0 {
			depth = 1
		}
		merged := make([]map[uint64]*transaction.PathElement, depth)
		for i := range merged {
			merged[i] = make(map[uint64]*transaction.PathElement)
		}
		var totalBytes uint64
		for _, txid := range txids {
			if err = ctx.Err(); err != nil {
				return basm.CompoundMerklePath{}, nil, err
			}
			position, ok := positions[txid]
			if !ok {
				return basm.CompoundMerklePath{}, nil, ErrBASMNotFound
			}
			remaining := uint64(s.limits.MaxProofBytes) - totalBytes
			if remaining == 0 {
				return basm.CompoundMerklePath{}, nil, basm.ErrLimitExceeded
			}
			proofBytes, proofErr := v.MerklePath(ctx, txid, uint32(remaining)) //nolint:gosec // remaining <= uint32 MaxProofBytes
			if proofErr != nil {
				return basm.CompoundMerklePath{}, nil, proofErr
			}
			if uint64(len(proofBytes)) > remaining {
				return basm.CompoundMerklePath{}, nil, basm.ErrLimitExceeded
			}
			totalBytes += uint64(len(proofBytes))
			path, proofErr := basm.ParseBoundedMerklePath(proofBytes, s.limits.MaxProofBytes)
			if proofErr != nil {
				return basm.CompoundMerklePath{}, nil, ErrBASMInvalidData
			}
			if proofErr = validateBASMProof(ctx, path, txid, position, header); proofErr != nil {
				return basm.CompoundMerklePath{}, nil, proofErr
			}
			for level, nodes := range path.Path {
				for _, node := range nodes {
					if old, exists := merged[level][node.Offset]; exists {
						if !sameProofNode(old, node) {
							return basm.CompoundMerklePath{}, nil, ErrBASMInvalidData
						}
						if node.Txid != nil && *node.Txid {
							old.Txid = node.Txid
						}
					} else {
						merged[level][node.Offset] = node
					}
				}
			}
		}
		levels := make([][]*transaction.PathElement, depth)
		for i, level := range merged {
			levels[i] = make([]*transaction.PathElement, 0, len(level))
			for _, node := range level {
				levels[i] = append(levels[i], node)
			}
			slices.SortFunc(levels[i], func(a, b *transaction.PathElement) int {
				if a.Offset < b.Offset {
					return -1
				}
				if a.Offset > b.Offset {
					return 1
				}
				return 0
			})
		}
		encoded := transaction.NewMerklePath(height, levels).Bytes()
		if uint64(len(encoded)) > uint64(s.limits.MaxProofBytes) || uint64(len(encoded))*2+uint64(len(txids))*67+uint64(len(topic))*6+256 > uint64(s.limits.MaxResponseBytes) {
			return basm.CompoundMerklePath{}, nil, basm.ErrLimitExceeded
		}
		return basm.CompoundMerklePath{Topic: topic, BlockHeight: height, TxIDs: slices.Clone(txids), MerklePath: hex.EncodeToString(encoded)}, []BASMCanonicalHeader{header}, nil
	})
}

func sameProofNode(a, b *transaction.PathElement) bool {
	if a.Hash == nil || b.Hash == nil {
		return a.Hash == nil && b.Hash == nil
	}
	return *a.Hash == *b.Hash
}

// validateBASMProof derives only parents whose children are already available,
// then follows the requested leaf iteratively. This avoids the SDK's recursive
// search through missing subtrees and its mixed-depth Combine assumptions.
func validateBASMProof(ctx context.Context, path *transaction.MerklePath, txid basm.Hash, position uint64, header BASMCanonicalHeader) error {
	depth := bits.Len64(header.TransactionCount - 1)
	if depth == 0 {
		depth = 1
	}
	if path.BlockHeight != header.Height || len(path.Path) == 0 || len(path.Path) > depth {
		return ErrBASMInvalidData
	}
	levels := make([]map[uint64]*chainhash.Hash, depth+1)
	for i := range levels {
		levels[i] = make(map[uint64]*chainhash.Hash)
	}
	width := header.TransactionCount
	for level, nodes := range path.Path {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, node := range nodes {
			if node == nil {
				return ErrBASMInvalidData
			}
			duplicate := node.Duplicate != nil && *node.Duplicate
			if duplicate {
				if node.Hash != nil || width <= 1 || width%2 == 0 || node.Offset != width {
					return ErrBASMInvalidData
				}
			} else if node.Hash == nil || node.Offset >= width {
				return ErrBASMInvalidData
			}
			if _, exists := levels[level][node.Offset]; exists {
				return ErrBASMInvalidData
			}
			levels[level][node.Offset] = node.Hash
		}
		width = width/2 + width%2
	}
	leaf, exists := levels[0][position]
	if !exists || leaf == nil || basm.Hash(*leaf) != txid {
		return ErrBASMInvalidData
	}
	if header.TransactionCount == 1 {
		if len(path.Path) != 1 || len(path.Path[0]) != 1 || position != 0 || txid != header.MerkleRoot {
			return ErrBASMInvalidData
		}
		return nil
	}
	for level := 0; level < depth; level++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		for offset, left := range levels[level] {
			if offset%2 != 0 || left == nil {
				continue
			}
			right, ok := levels[level][offset+1]
			if !ok {
				continue
			}
			if right == nil {
				right = left
			}
			parent := transaction.MerkleTreeParent(left, right)
			old, exists := levels[level+1][offset/2]
			if exists && (old == nil || *old != *parent) {
				return ErrBASMInvalidData
			}
			levels[level+1][offset/2] = parent
		}
	}
	working := leaf
	for level := 0; level < depth; level++ {
		sibling, ok := levels[level][(position>>level)^1]
		if !ok {
			return ErrBASMInvalidData
		}
		if sibling == nil {
			sibling = working
		}
		if (position>>level)%2 == 0 {
			working = transaction.MerkleTreeParent(working, sibling)
		} else {
			working = transaction.MerkleTreeParent(sibling, working)
		}
	}
	if working == nil || basm.Hash(*working) != header.MerkleRoot {
		return ErrBASMInvalidData
	}
	return nil
}

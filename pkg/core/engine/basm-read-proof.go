package engine

import (
	"cmp"
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
		return s.compoundMerklePath(ctx, v, topic, height, txids)
	})
}

func (s *BASMReadService) compoundMerklePath(ctx context.Context, v BASMReadView, topic string, height uint32, txids []basm.Hash) (basm.CompoundMerklePath, []BASMCanonicalHeader, error) {
	_, admitted, header, err := s.readBlock(ctx, v, topic, height)
	if err != nil {
		return basm.CompoundMerklePath{}, nil, err
	}
	merged, err := s.mergeRequestedProofs(ctx, v, txids, admittedPositions(admitted), header)
	if err != nil {
		return basm.CompoundMerklePath{}, nil, err
	}
	encoded := encodeMergedProof(height, merged, merkleProofDepth(header.TransactionCount))
	if proofResponseTooLarge(encoded, txids, topic, s.limits) {
		return basm.CompoundMerklePath{}, nil, basm.ErrLimitExceeded
	}
	return basm.CompoundMerklePath{Topic: topic, BlockHeight: height, TxIDs: slices.Clone(txids), MerklePath: hex.EncodeToString(encoded)}, []BASMCanonicalHeader{header}, nil
}

func admittedPositions(admitted []basm.AdmittedTxRef) map[basm.Hash]uint64 {
	positions := make(map[basm.Hash]uint64, len(admitted))
	for _, ref := range admitted {
		positions[ref.TxID] = ref.BlockIndex
	}
	return positions
}

func merkleProofDepth(txCount uint64) int {
	depth := bits.Len64(txCount - 1)
	if depth == 0 {
		return 1
	}
	return depth
}

func (s *BASMReadService) mergeRequestedProofs(ctx context.Context, v BASMReadView, txids []basm.Hash, positions map[basm.Hash]uint64, header BASMCanonicalHeader) ([]map[uint64]*transaction.PathElement, error) {
	depth := merkleProofDepth(header.TransactionCount)
	merged := make([]map[uint64]*transaction.PathElement, depth)
	for i := range merged {
		merged[i] = make(map[uint64]*transaction.PathElement)
	}
	var totalBytes uint64
	for _, txid := range txids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		added, err := s.mergeOneProof(ctx, v, txid, positions, header, merged, totalBytes)
		if err != nil {
			return nil, err
		}
		totalBytes += added
	}
	return merged, nil
}

func (s *BASMReadService) mergeOneProof(ctx context.Context, v BASMReadView, txid basm.Hash, positions map[basm.Hash]uint64, header BASMCanonicalHeader, merged []map[uint64]*transaction.PathElement, totalBytes uint64) (uint64, error) {
	position, ok := positions[txid]
	if !ok {
		return 0, ErrBASMNotFound
	}
	remaining := uint64(s.limits.MaxProofBytes) - totalBytes
	if remaining == 0 {
		return 0, basm.ErrLimitExceeded
	}
	proofBytes, err := v.MerklePath(ctx, txid, uint32(remaining)) //nolint:gosec // remaining <= uint32 MaxProofBytes
	if err != nil {
		return 0, err
	}
	if uint64(len(proofBytes)) > remaining {
		return 0, basm.ErrLimitExceeded
	}
	path, err := basm.ParseBoundedMerklePath(proofBytes, s.limits.MaxProofBytes)
	if err != nil {
		return 0, ErrBASMInvalidData
	}
	if err = validateBASMProof(ctx, path, txid, position, header); err != nil {
		return 0, err
	}
	if err = mergeProofLevels(merged, path); err != nil {
		return 0, err
	}
	return uint64(len(proofBytes)), nil
}

func mergeProofLevels(merged []map[uint64]*transaction.PathElement, path *transaction.MerklePath) error {
	for level, nodes := range path.Path {
		for _, node := range nodes {
			if err := mergeProofNode(merged[level], node); err != nil {
				return err
			}
		}
	}
	return nil
}

func mergeProofNode(level map[uint64]*transaction.PathElement, node *transaction.PathElement) error {
	old, exists := level[node.Offset]
	if !exists {
		level[node.Offset] = node
		return nil
	}
	if !sameProofNode(old, node) {
		return ErrBASMInvalidData
	}
	if node.Txid != nil && *node.Txid {
		old.Txid = node.Txid
	}
	return nil
}

func encodeMergedProof(height uint32, merged []map[uint64]*transaction.PathElement, depth int) []byte {
	levels := make([][]*transaction.PathElement, depth)
	for i, level := range merged {
		levels[i] = make([]*transaction.PathElement, 0, len(level))
		for _, node := range level {
			levels[i] = append(levels[i], node)
		}
		slices.SortFunc(levels[i], func(a, b *transaction.PathElement) int {
			return cmp.Compare(a.Offset, b.Offset)
		})
	}
	return transaction.NewMerklePath(height, levels).Bytes()
}

func proofResponseTooLarge(encoded []byte, txids []basm.Hash, topic string, limits basm.ReadLimits) bool {
	return uint64(len(encoded)) > uint64(limits.MaxProofBytes) || uint64(len(encoded))*2+uint64(len(txids))*67+uint64(len(topic))*6+256 > uint64(limits.MaxResponseBytes)
}

func sameProofNode(a, b *transaction.PathElement) bool {
	if a.Hash == nil || b.Hash == nil {
		return a.Hash == nil && b.Hash == nil
	}
	return *a.Hash == *b.Hash
}

// validateBASMProof derives only parents whose children are already available,
// then checks every supplied base path iteratively. This avoids the SDK's recursive
// search through missing subtrees and its mixed-depth Combine assumptions.
func validateBASMProof(ctx context.Context, path *transaction.MerklePath, txid basm.Hash, position uint64, header BASMCanonicalHeader) error {
	depth := merkleProofDepth(header.TransactionCount)
	if path.BlockHeight != header.Height || len(path.Path) == 0 || len(path.Path) > depth {
		return ErrBASMInvalidData
	}
	levels, baseOffsets, err := indexProofNodes(ctx, path, header.TransactionCount, depth)
	if err != nil {
		return err
	}
	if err = checkProofLeaf(levels, path, txid, position, header); err != nil {
		return err
	}
	if header.TransactionCount == 1 {
		return nil
	}
	if err = deriveProofParents(ctx, levels, depth); err != nil {
		return err
	}
	root := levels[depth][0]
	if root == nil || basm.Hash(*root) != header.MerkleRoot {
		return ErrBASMInvalidData
	}
	return walkProofBaseOffsets(ctx, levels, baseOffsets, depth)
}

func indexProofNodes(ctx context.Context, path *transaction.MerklePath, txCount uint64, depth int) ([]map[uint64]*chainhash.Hash, []uint64, error) {
	levels := make([]map[uint64]*chainhash.Hash, depth+1)
	for i := range levels {
		levels[i] = make(map[uint64]*chainhash.Hash)
	}
	width := txCount
	baseOffsets := make([]uint64, 0, len(path.Path[0]))
	for level, nodes := range path.Path {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		for _, node := range nodes {
			if err := indexProofNode(levels, &baseOffsets, node, level, width); err != nil {
				return nil, nil, err
			}
		}
		if level == 0 {
			slices.Sort(baseOffsets)
		}
		width = width/2 + width%2
	}
	return levels, baseOffsets, nil
}

func indexProofNode(levels []map[uint64]*chainhash.Hash, baseOffsets *[]uint64, node *transaction.PathElement, level int, width uint64) error {
	if node == nil {
		return ErrBASMInvalidData
	}
	duplicate := node.Duplicate != nil && *node.Duplicate
	if err := validateProofNodePlacement(node, duplicate, width); err != nil {
		return err
	}
	if level == 0 {
		if !duplicate {
			*baseOffsets = append(*baseOffsets, node.Offset)
		}
	} else if !legalProofAncestor(node.Offset, level, *baseOffsets) {
		return ErrBASMInvalidData
	}
	if _, exists := levels[level][node.Offset]; exists {
		return ErrBASMInvalidData
	}
	levels[level][node.Offset] = node.Hash
	return nil
}

func validateProofNodePlacement(node *transaction.PathElement, duplicate bool, width uint64) error {
	if duplicate {
		if node.Hash != nil || width <= 1 || width%2 == 0 || node.Offset != width {
			return ErrBASMInvalidData
		}
		return nil
	}
	if node.Hash == nil || node.Offset >= width {
		return ErrBASMInvalidData
	}
	return nil
}

func legalProofAncestor(offset uint64, level int, baseOffsets []uint64) bool {
	// BRC-74/TS internal offsets must be siblings of ancestors of a supplied
	// base hash. A consistent own-parent node is still an invalid wire shape
	// when no base leaf needs it.
	_, legal := slices.BinarySearchFunc(baseOffsets, offset^1, func(base, ancestor uint64) int {
		return cmp.Compare(base>>level, ancestor)
	})
	return legal
}

func checkProofLeaf(levels []map[uint64]*chainhash.Hash, path *transaction.MerklePath, txid basm.Hash, position uint64, header BASMCanonicalHeader) error {
	leaf, exists := levels[0][position]
	if !exists || leaf == nil || basm.Hash(*leaf) != txid {
		return ErrBASMInvalidData
	}
	if header.TransactionCount != 1 {
		return nil
	}
	if len(path.Path) != 1 || len(path.Path[0]) != 1 || position != 0 || txid != header.MerkleRoot {
		return ErrBASMInvalidData
	}
	return nil
}

func deriveProofParents(ctx context.Context, levels []map[uint64]*chainhash.Hash, depth int) error {
	for level := 0; level < depth; level++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := deriveProofLevel(levels, level); err != nil {
			return err
		}
	}
	return nil
}

func deriveProofLevel(levels []map[uint64]*chainhash.Hash, level int) error {
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
	return nil
}

func walkProofBaseOffsets(ctx context.Context, levels []map[uint64]*chainhash.Hash, baseOffsets []uint64, depth int) error {
	// TS validates every supplied base hash, including sibling txids that were
	// not requested. Each must connect to the same resolved root. Parent hashes
	// were already derived and checked above, so this walk never rehashes or
	// recursively explores absent subtrees.
	for i, offset := range baseOffsets {
		if i%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if !connectedProofBase(levels, offset, depth) {
			return ErrBASMInvalidData
		}
	}
	return nil
}

func connectedProofBase(levels []map[uint64]*chainhash.Hash, offset uint64, depth int) bool {
	for level := 0; level < depth; level++ {
		if _, exists := levels[level][(offset>>level)^1]; !exists || levels[level+1][offset>>(level+1)] == nil {
			return false
		}
	}
	return true
}

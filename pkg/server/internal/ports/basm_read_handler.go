package ports

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"mime"
	"strings"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"

	"github.com/bsv-blockchain/go-overlay-services/pkg/core/basm"
	"github.com/bsv-blockchain/go-overlay-services/pkg/core/engine"
)

// BASMReadHandler adapts the optional BASM read capability to the public BRC-136 JSON routes.
// It does not make a claim about agreement, persistence, or chain verification.
type BASMReadHandler struct {
	provider  engine.BASMProvider
	limits    basm.ReadLimits
	configErr error
}

// NewBASMReadHandler creates a bounded handler. A nil provider deliberately exposes
// the routes as unsupported rather than silently using another engine capability.
func NewBASMReadHandler(provider engine.BASMProvider, limits basm.ReadLimits) *BASMReadHandler {
	if limits == (basm.ReadLimits{}) {
		limits = basm.DefaultReadLimits()
	}
	if !engine.IsBASMProviderAvailable(provider) {
		provider = nil
	}
	return &BASMReadHandler{provider: provider, limits: limits, configErr: limits.Validate()}
}

// Handle reads, validates, and dispatches one BASM JSON request.
func (h *BASMReadHandler) Handle(c *fiber.Ctx, kind string, topicRequired bool) error {
	if h.configErr != nil {
		return h.writeError(c, fiber.StatusInternalServerError, "invalid_data", "BASM handler configuration is invalid")
	}
	if !isJSONContentType(c.Get(fiber.HeaderContentType)) {
		return h.writeError(c, fiber.StatusBadRequest, "invalid_request", "request must use application/json")
	}
	if uint64(len(c.Body())) > uint64(h.limits.MaxRequestBytes) {
		return h.writeError(c, fiber.StatusRequestEntityTooLarge, "request_too_large", "request exceeds configured limit")
	}

	topic, err := h.topic(c, topicRequired)
	if err != nil {
		return h.writeMappedError(c, err)
	}
	request, err := basm.DecodeReadRequest(c.Body(), kind, h.limits)
	if err != nil {
		return h.writeInputError(c, err)
	}
	if h.provider == nil {
		return h.writeError(c, fiber.StatusNotImplemented, "unsupported", "BASM reads are not supported")
	}

	ctx, cancel := context.WithTimeout(c.UserContext(), h.limits.RequestTimeout)
	defer cancel()
	if err = ctx.Err(); err != nil {
		return h.writeMappedError(c, err)
	}

	var response any
	switch kind {
	case "tip":
		response, err = h.provider.ProvideTopicAnchorTip(ctx, topic)
	case "range":
		response, err = h.provider.ProvideTopicAnchorRange(ctx, topic, request.FromHeight, request.ToHeight)
	case "list":
		response, err = h.provider.ProvideAdmittedList(ctx, topic, request.BlockHeight, request.BlockHash)
	case "proof":
		response, err = h.provider.ProvideCompoundMerklePath(ctx, topic, request.BlockHeight, request.TxIDs)
	case "raw":
		response, err = h.provider.ProvideRawTransactions(ctx, request.TxIDs)
	default:
		return h.writeError(c, fiber.StatusBadRequest, "invalid_request", "unknown BASM request")
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return h.writeMappedError(c, ctxErr)
	}
	if err != nil {
		return h.writeMappedError(c, err)
	}
	if err = h.validateResponse(kind, topic, request, response); err != nil {
		return h.writeMappedError(c, err)
	}
	if err = ctx.Err(); err != nil {
		return h.writeMappedError(c, err)
	}

	encoded, err := json.Marshal(response)
	if err != nil {
		return h.writeError(c, fiber.StatusInternalServerError, "invalid_data", "BASM provider returned invalid data")
	}
	if uint64(len(encoded)) > uint64(h.limits.MaxResponseBytes) {
		return h.writeError(c, fiber.StatusRequestEntityTooLarge, "response_too_large", "response exceeds configured limit")
	}
	if err = ctx.Err(); err != nil {
		return h.writeMappedError(c, err)
	}
	return c.Status(fiber.StatusOK).Type(fiber.MIMEApplicationJSON).Send(encoded)
}

func (h *BASMReadHandler) topic(c *fiber.Ctx, required bool) (string, error) {
	if !required {
		return "", nil
	}
	var values []string
	for name, headerValues := range c.GetReqHeaders() {
		if strings.EqualFold(name, "x-bsv-topic") {
			values = append(values, headerValues...)
		}
	}
	if len(values) != 1 || values[0] == "" || !utf8.ValidString(values[0]) || uint64(len(values[0])) > uint64(h.limits.MaxTopicBytes) {
		return "", basm.ErrInvalidInput
	}
	return values[0], nil
}

func (h *BASMReadHandler) validateResponse(kind, topic string, request basm.ReadRequest, response any) error {
	budget := responseBudget{limit: uint64(h.limits.MaxResponseBytes), ok: true}
	switch value := response.(type) {
	case basm.TopicAnchorTip:
		if kind != "tip" || value.Topic != topic || !h.validTopic(value.Topic) || !h.validTip(value) {
			return engine.ErrBASMInvalidData
		}
		budget.add(96)
		budget.addString(value.Topic)
		budget.addHashes(value.BlockHash, value.BASMRoot)
		budget.add(86) // admitted count, TAC, and JSON punctuation
	case basm.TopicAnchorRange:
		count := uint64(request.ToHeight) - uint64(request.FromHeight) + 1
		if kind != "range" || value.Topic != topic || !h.validTopic(value.Topic) || value.Anchors == nil || uint64(len(value.Anchors)) != count || uint64(len(value.Anchors)) > uint64(h.limits.MaxRange) {
			return engine.ErrBASMInvalidData
		}
		budget.add(48)
		budget.addString(value.Topic)
		for index, anchor := range value.Anchors {
			if anchor.Topic != topic || uint64(anchor.BlockHeight) != uint64(request.FromHeight)+uint64(index) || !h.validTopic(anchor.Topic) || anchor.Validate(h.limits.Limits) != nil {
				return engine.ErrBASMInvalidData
			}
			budget.add(132)
			budget.addString(anchor.Topic)
			budget.add(66 * 3) // block hash, BASM root, TAC
		}
	case basm.AdmittedList:
		if kind != "list" || value.Topic != topic || value.BlockHeight != request.BlockHeight || !h.validTopic(value.Topic) || value.Admitted == nil || uint64(len(value.Admitted)) > uint64(h.limits.MaxAdmitted) || (request.BlockHash != nil && (value.BlockHash == nil || *value.BlockHash != *request.BlockHash)) {
			return engine.ErrBASMInvalidData
		}
		budget.add(112)
		budget.addString(value.Topic)
		budget.add(66)
		budget.addRepeated(uint64(len(value.Admitted)), 128)
		if !budget.ok {
			return basm.ErrLimitExceeded
		}
		if !validAdmitted(value.Admitted) {
			return engine.ErrBASMInvalidData
		}
	case basm.CompoundMerklePath:
		if kind != "proof" || value.Topic != topic || value.BlockHeight != request.BlockHeight || !h.validTopic(value.Topic) || value.TxIDs == nil || len(value.TxIDs) != len(request.TxIDs) || uint64(len(value.TxIDs)) > uint64(h.limits.MaxRequestedTxIDs) || !sameHashes(value.TxIDs, request.TxIDs) || uint64(len(value.MerklePath)) > uint64(h.limits.MaxProofBytes)*2 {
			return engine.ErrBASMInvalidData
		}
		budget.add(112)
		budget.addString(value.Topic)
		budget.addRepeated(uint64(len(value.TxIDs)), 67)
		budget.addLiteralString(value.MerklePath)
		if !budget.ok {
			return basm.ErrLimitExceeded
		}
		if !validLowerHex(value.MerklePath) {
			return engine.ErrBASMInvalidData
		}
	case basm.RawTransactions:
		transactionsCount := uint64(len(value.Transactions))
		missingCount := uint64(len(value.Missing))
		if kind != "raw" || value.Transactions == nil || value.Missing == nil || transactionsCount > uint64(h.limits.MaxRequestedTxIDs) || missingCount > uint64(h.limits.MaxRequestedTxIDs)-transactionsCount || !validRawMapping(request.TxIDs, value) {
			return engine.ErrBASMInvalidData
		}
		budget.add(48)
		for _, transaction := range value.Transactions {
			if uint64(len(transaction.RawTx)) > uint64(h.limits.MaxRawTxBytes)*2 {
				return engine.ErrBASMInvalidData
			}
			budget.add(128)
			budget.addLiteralString(transaction.RawTx)
		}
		budget.addRepeated(uint64(len(value.Missing)), 67)
		if !budget.ok {
			return basm.ErrLimitExceeded
		}
		for _, transaction := range value.Transactions {
			if !validLowerHex(transaction.RawTx) {
				return engine.ErrBASMInvalidData
			}
		}
	default:
		return engine.ErrBASMInvalidData
	}
	if !budget.ok {
		return basm.ErrLimitExceeded
	}
	return nil
}

func (h *BASMReadHandler) validTip(value basm.TopicAnchorTip) bool {
	if value.BlockHeight == -1 {
		return value.BlockHash == nil && value.BASMRoot == nil && value.AdmittedCount == nil && value.TAC == (basm.Hash{})
	}
	return value.BlockHeight >= 0 && value.BlockHeight <= math.MaxUint32 && (value.AdmittedCount == nil || *value.AdmittedCount <= uint64(h.limits.MaxAdmitted))
}

func validAdmitted(admitted []basm.AdmittedTxRef) bool {
	seen := make(map[basm.Hash]struct{}, len(admitted))
	var previous uint64
	for index, ref := range admitted {
		if ref.BlockIndex > basm.MaxSafeJSONInteger || (index > 0 && ref.BlockIndex <= previous) {
			return false
		}
		if _, exists := seen[ref.TxID]; exists {
			return false
		}
		seen[ref.TxID] = struct{}{}
		previous = ref.BlockIndex
	}
	return true
}

func sameHashes(left, right []basm.Hash) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validRawMapping(requested []basm.Hash, value basm.RawTransactions) bool {
	seen := make(map[basm.Hash]struct{}, len(requested))
	for _, txid := range requested {
		if _, exists := seen[txid]; exists {
			return false
		}
		seen[txid] = struct{}{}
	}
	returned := make(map[basm.Hash]struct{}, len(requested))
	for _, record := range value.Transactions {
		if _, requestedID := seen[record.TxID]; !requestedID {
			return false
		}
		if _, duplicate := returned[record.TxID]; duplicate {
			return false
		}
		returned[record.TxID] = struct{}{}
	}
	for _, txid := range value.Missing {
		if _, requestedID := seen[txid]; !requestedID {
			return false
		}
		if _, duplicate := returned[txid]; duplicate {
			return false
		}
		returned[txid] = struct{}{}
	}
	return len(returned) == len(seen)
}

func validLowerHex(value string) bool {
	if len(value) == 0 || len(value)%2 != 0 {
		return false
	}
	for index := range value {
		if (value[index] < '0' || value[index] > '9') && (value[index] < 'a' || value[index] > 'f') {
			return false
		}
	}
	return true
}

type responseBudget struct {
	limit uint64
	size  uint64
	ok    bool
}

func (b *responseBudget) add(value uint64) {
	if !b.ok {
		return
	}
	if value > b.limit-b.size {
		b.ok = false
		return
	}
	b.size += value
	b.ok = true
}

func (b *responseBudget) addRepeated(count, size uint64) {
	if !b.ok {
		return
	}
	if count > 0 && size > (b.limit-b.size)/count {
		b.ok = false
		return
	}
	b.add(count * size)
}

func (b *responseBudget) addString(value string) {
	if !b.ok || b.limit-b.size < 2 || uint64(len(value)) > (b.limit-b.size-2)/6 { // JSON escaping can expand every byte to six bytes.
		b.ok = false
		return
	}
	b.add(uint64(len(value))*6 + 2)
}

func (b *responseBudget) addLiteralString(value string) {
	b.add(uint64(len(value)) + 2)
}

func (b *responseBudget) addHashes(hashes ...*basm.Hash) {
	for _, hash := range hashes {
		if hash != nil {
			b.add(66)
		}
	}
}

func (h *BASMReadHandler) validTopic(topic string) bool {
	return topic != "" && utf8.ValidString(topic) && uint64(len(topic)) <= uint64(h.limits.MaxTopicBytes)
}

func (h *BASMReadHandler) writeMappedError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return h.writeError(c, fiber.StatusRequestTimeout, "cancelled", "request was cancelled")
	case errors.Is(err, context.DeadlineExceeded):
		return h.writeError(c, fiber.StatusGatewayTimeout, "timeout", "request timed out")
	case errors.Is(err, basm.ErrLimitExceeded):
		return h.writeError(c, fiber.StatusRequestEntityTooLarge, "limit_exceeded", "request exceeds configured limit")
	case errors.Is(err, engine.ErrBASMInvalidData):
		return h.writeError(c, fiber.StatusInternalServerError, "invalid_data", "BASM provider returned invalid data")
	case errors.Is(err, basm.ErrInvalidInput), errors.Is(err, basm.ErrInvalidHash):
		return h.writeError(c, fiber.StatusBadRequest, "invalid_request", "request is invalid")
	case errors.Is(err, engine.ErrBASMUnsupported):
		return h.writeError(c, fiber.StatusNotImplemented, "unsupported", "BASM reads are not supported")
	case errors.Is(err, engine.ErrBASMNotReady):
		return h.writeError(c, fiber.StatusServiceUnavailable, "not_ready", "BASM data is not ready")
	case errors.Is(err, engine.ErrBASMNotFound):
		return h.writeError(c, fiber.StatusNotFound, "not_found", "BASM record was not found")
	default:
		return h.writeError(c, fiber.StatusInternalServerError, "invalid_data", "BASM provider returned invalid data")
	}
}

func (h *BASMReadHandler) writeInputError(c *fiber.Ctx, err error) error {
	if errors.Is(err, basm.ErrLimitExceeded) {
		return h.writeError(c, fiber.StatusRequestEntityTooLarge, "limit_exceeded", "request exceeds configured limit")
	}
	return h.writeError(c, fiber.StatusBadRequest, "invalid_request", "request is invalid")
}

func (h *BASMReadHandler) writeError(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(basmErrorResponse{Status: "error", Code: code, Message: message})
}

func isJSONContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == fiber.MIMEApplicationJSON
}

type basmErrorResponse struct {
	Status  string `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

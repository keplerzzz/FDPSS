package fdvss

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	mathrand "math/rand"
	"os"
	"strconv"
	"strings"
	"sync"

	"go-fdvss-fdpss/primitives"
)

const (
	rngModeCrypto = "crypto"
	rngModeFast   = "fast"
)

type rngConfig struct {
	mode string
	seed int64
}

var (
	rngInitOnce sync.Once
	rngCfg      rngConfig
	fastRandMu  sync.Mutex
	fastRand    *mathrand.Rand
)

func loadRNGConfig() {
	rngCfg = rngConfig{mode: rngModeCrypto, seed: 1}
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("RNG_MODE")))
	if mode == "" || mode == rngModeCrypto {
		rngCfg.mode = rngModeCrypto
		return
	}
	if mode != rngModeFast {
		rngCfg.mode = rngModeCrypto
		return
	}
	rngCfg.mode = rngModeFast
	if seedText := strings.TrimSpace(os.Getenv("RNG_SEED")); seedText != "" {
		if parsed, err := strconv.ParseInt(seedText, 10, 64); err == nil {
			rngCfg.seed = parsed
		}
	}
	fastRand = mathrand.New(mathrand.NewSource(rngCfg.seed))
}

func getRNGConfig() rngConfig {
	rngInitOnce.Do(loadRNGConfig)
	return rngCfg
}

func RandomNonZeroMod(f *FieldParams) (uint64, error) {
	if f == nil {
		return 0, fmt.Errorf("nil field params")
	}
	p := f.P
	if p < 3 {
		return 0, fmt.Errorf("field too small for RandomNonZeroMod")
	}
	cfg := getRNGConfig()
	if cfg.mode == rngModeFast {
		fastRandMu.Lock()
		defer fastRandMu.Unlock()
		u := fastRand.Uint64()
		return u%(p-1) + 1, nil
	}
	var buf [8]byte
	if _, err := io.ReadFull(rand.Reader, buf[:]); err != nil {
		return 0, err
	}
	u := binary.LittleEndian.Uint64(buf[:])
	return u%(p-1) + 1, nil
}

type Share struct {
	Index uint64
	Value uint64
}

type shamirBatchEvaluator struct {
	field  *FieldParams
	params *primitives.Params
	t      int
	xPows  [][]uint64
}

func newShamirBatchEvaluator(field *FieldParams, params *primitives.Params) (*shamirBatchEvaluator, error) {
	if field == nil {
		return nil, fmt.Errorf("nil field params")
	}
	if params == nil {
		return nil, fmt.Errorf("nil params")
	}
	if params.D < 0 {
		return nil, fmt.Errorf("invalid degree %d", params.D)
	}
	if params.N <= 0 {
		return nil, fmt.Errorf("no receivers configured")
	}
	t := params.D + 1
	xPows := make([][]uint64, params.N)
	for seq := 0; seq < params.N; seq++ {
		x := field.Uint64FromUint64(uint64(seq + 1))
		xPows[seq] = make([]uint64, t)
		xPows[seq][0] = Uint64One()
		for exp := 1; exp < t; exp++ {
			xPows[seq][exp] = field.ModMulCanon(xPows[seq][exp-1], x)
		}
	}
	return &shamirBatchEvaluator{
		field:  field,
		params: params,
		t:      t,
		xPows:  xPows,
	}, nil
}

func (e *shamirBatchEvaluator) sampleCoeffs(secret uint64) ([]uint64, error) {
	f := e.field
	coeffs := make([]uint64, e.t)
	coeffs[0] = secret % f.P
	if e.t <= 1 {
		return coeffs, nil
	}
	randomCoeffs, err := sampleRandomModValuesBatch(f, e.t-1)
	if err != nil {
		return nil, err
	}
	for i := 1; i < e.t; i++ {
		coeffs[i] = randomCoeffs[i-1]
	}
	return coeffs, nil
}

func (e *shamirBatchEvaluator) evaluateCoeffsAtAllPoints(coeffs []uint64) []uint64 {
	f := e.field
	values := make([]uint64, e.params.N)
	for seq := 0; seq < e.params.N; seq++ {
		sum := Uint64Zero()
		for exp := 0; exp < len(coeffs); exp++ {
			sum = f.ModAddCanon(sum, f.ModMulCanon(coeffs[exp], e.xPows[seq][exp]))
		}
		values[seq] = sum
	}
	return values
}

func (e *shamirBatchEvaluator) generateShareValuesBatch(secrets []uint64) ([][]uint64, error) {
	if len(secrets) == 0 {
		return nil, nil
	}
	f := e.field
	out := make([][]uint64, len(secrets))
	var randomBatch []uint64
	if e.t > 1 {
		var err error
		randomBatch, err = sampleRandomModValuesBatch(f, len(secrets)*(e.t-1))
		if err != nil {
			return nil, err
		}
	}
	randomOffset := 0
	for i, secret := range secrets {
		coeffs := make([]uint64, e.t)
		coeffs[0] = secret % f.P
		for j := 1; j < e.t; j++ {
			coeffs[j] = randomBatch[randomOffset]
			randomOffset++
		}
		out[i] = e.evaluateCoeffsAtAllPoints(coeffs)
	}
	return out, nil
}

func generateShares(field *FieldParams, params *primitives.Params, secret uint64) ([]Share, error) {
	eval, err := newShamirBatchEvaluator(field, params)
	if err != nil {
		return nil, err
	}
	valuesBatch, err := eval.generateShareValuesBatch([]uint64{secret})
	if err != nil {
		return nil, err
	}
	shares := make([]Share, eval.params.N)
	for i := 0; i < eval.params.N; i++ {
		shares[i] = Share{
			Index: field.Uint64FromUint64(uint64(i + 1)),
			Value: valuesBatch[0][i],
		}
	}
	return shares, nil
}

func shareSinglePoly(field *FieldParams, params *primitives.Params, coeffs []uint64) ([][]uint64, error) {
	eval, err := newShamirBatchEvaluator(field, params)
	if err != nil {
		return nil, err
	}
	return shareSinglePolyWithEvaluator(eval, coeffs)
}

func shareSinglePolyWithEvaluator(eval *shamirBatchEvaluator, coeffs []uint64) ([][]uint64, error) {
	receiverCount := eval.params.N
	out := make([][]uint64, receiverCount)
	for i := range out {
		out[i] = make([]uint64, len(coeffs))
	}
	coeffShares, err := eval.generateShareValuesBatch(coeffs)
	if err != nil {
		return nil, err
	}
	for coeffIdx := range coeffs {
		for seq := 0; seq < receiverCount; seq++ {
			out[seq][coeffIdx] = coeffShares[coeffIdx][seq]
		}
	}
	return out, nil
}

func shareFutureMatrixByGenerateShares(field *FieldParams, params *primitives.Params, matrix [][]uint64) ([][][]uint64, error) {
	eval, err := newShamirBatchEvaluator(field, params)
	if err != nil {
		return nil, err
	}
	return shareFutureMatrixWithEvaluator(eval, params, matrix)
}

func shareFutureMatrixWithEvaluator(eval *shamirBatchEvaluator, params *primitives.Params, matrix [][]uint64) ([][][]uint64, error) {
	receiverCount := eval.params.N
	size := params.D + 1
	if len(matrix) != size {
		return nil, fmt.Errorf("future matrix rows %d != %d", len(matrix), size)
	}
	for r := range matrix {
		if len(matrix[r]) != size {
			return nil, fmt.Errorf("future matrix row %d has %d cols != %d", r, len(matrix[r]), size)
		}
	}
	out := make([][][]uint64, receiverCount)
	for i := 0; i < receiverCount; i++ {
		out[i] = make([][]uint64, size)
		for x := 0; x < size; x++ {
			out[i][x] = make([]uint64, size)
		}
	}
	flatSecrets := make([]uint64, 0, size*size)
	for x := 0; x < size; x++ {
		for z := 0; z < size; z++ {
			flatSecrets = append(flatSecrets, matrix[x][z])
		}
	}
	flatShares, err := eval.generateShareValuesBatch(flatSecrets)
	if err != nil {
		return nil, err
	}
	flatIdx := 0
	for x := 0; x < size; x++ {
		for z := 0; z < size; z++ {
			for seq := 0; seq < receiverCount; seq++ {
				out[seq][x][z] = flatShares[flatIdx][seq]
			}
			flatIdx++
		}
	}
	return out, nil
}

func sampleRandomModValuesBatch(field *FieldParams, count int) ([]uint64, error) {
	if field == nil {
		return nil, fmt.Errorf("nil field params")
	}
	mod := field.P
	if count < 0 {
		return nil, fmt.Errorf("invalid random value count %d", count)
	}
	if count == 0 {
		return []uint64{}, nil
	}
	cfg := getRNGConfig()
	if cfg.mode == rngModeFast {
		values := make([]uint64, count)
		fastRandMu.Lock()
		defer fastRandMu.Unlock()
		for i := 0; i < count; i++ {
			values[i] = fastRand.Uint64() % mod
		}
		return values, nil
	}
	values := make([]uint64, count)
	byteBuf := make([]byte, count*8)
	if _, err := io.ReadFull(rand.Reader, byteBuf); err != nil {
		return nil, fmt.Errorf("batch random read failed: %w", err)
	}
	for i := 0; i < count; i++ {
		raw := binary.LittleEndian.Uint64(byteBuf[i*8 : (i+1)*8])
		values[i] = raw % mod
	}
	return values, nil
}

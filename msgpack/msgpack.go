package msgpack

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"reflect"

	"github.com/ugorji/go/codec"
)

var CodecHandle *codec.MsgpackHandle

func init() {
	CodecHandle = new(codec.MsgpackHandle)
	CodecHandle.ErrorIfNoField = true
	CodecHandle.ErrorIfNoArrayExpand = true
	CodecHandle.Canonical = true
	CodecHandle.RecursiveEmptyCheck = true
	CodecHandle.WriteExt = true
	CodecHandle.PositiveIntUnsigned = true
}

func Encode(obj interface{}) []byte {
	if obj == nil {
		return nil
	}
	v := reflect.ValueOf(obj)
	v, ok := stripIfaceAndPtrChain(v)
	if !ok || !v.IsValid() {
		return nil
	}
	return appendEncodeVarint(nil, v)
}

func Decode(data []byte, objptr interface{}) error {
	if objptr == nil {
		return fmt.Errorf("nil decode target")
	}
	rv := reflect.ValueOf(objptr)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("Decode target must be non-nil pointer")
	}
	rv = rv.Elem()
	off := 0
	if err := decodeVarintValue(data, &off, rv); err != nil {
		return err
	}
	if off != len(data) {
		return fmt.Errorf("trailing data: got %d extra bytes after position %d", len(data)-off, off)
	}
	return nil
}

func msgpackChunkFromReflect(v reflect.Value) interface{} {
	if v.Kind() == reflect.Ptr && v.IsNil() {
		return reflect.Zero(v.Type()).Interface()
	}
	return v.Interface()
}

func NewDecoder(r io.Reader) *codec.Decoder {
	return codec.NewDecoder(r, CodecHandle)
}

func encodeMsgpackChunk(obj interface{}) []byte {
	var b []byte
	enc := codec.NewEncoderBytes(&b, CodecHandle)
	enc.MustEncode(obj)
	return b
}

func stripIfaceAndPtrChain(v reflect.Value) (reflect.Value, bool) {
	for {
		switch v.Kind() {
		case reflect.Interface:
			if v.IsNil() {
				return v, false
			}
			v = v.Elem()
		case reflect.Ptr:
			if v.IsNil() {
				return v, false
			}
			v = v.Elem()
		default:
			return v, true
		}
	}
}

func appendUvarint(dst []byte, x uint64) []byte {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], x)
	return append(dst, buf[:n]...)
}

func appendZigzagInt64(dst []byte, n int64) []byte {
	return appendUvarint(dst, zigzagUint64(n))
}

func appendEncodeVarint(dst []byte, v reflect.Value) []byte {
	for v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Bool:
		b := byte(0)
		if v.Bool() {
			b = 1
		}
		return append(dst, b)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return appendZigzagInt64(dst, v.Int())

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return appendUvarint(dst, v.Uint())

	case reflect.Float32:
		var bits [4]byte
		binary.LittleEndian.PutUint32(bits[:], uint32(math.Float32bits(float32(v.Float()))))
		return append(dst, bits[:]...)

	case reflect.Float64:
		var bits [8]byte
		binary.LittleEndian.PutUint64(bits[:], math.Float64bits(v.Float()))
		return append(dst, bits[:]...)

	case reflect.String:
		s := v.String()
		dst = appendUvarint(dst, uint64(len(s)))
		return append(dst, s...)

	case reflect.Slice:
		n := v.Len()
		dst = appendUvarint(dst, uint64(n))
		if v.Type().Elem().Kind() == reflect.Uint8 {
			if n == 0 {
				return dst
			}
			return append(dst, v.Bytes()...)
		}
		for i := 0; i < n; i++ {
			dst = appendEncodeVarint(dst, v.Index(i))
		}
		return dst

	case reflect.Array:
		n := v.Len()
		dst = appendUvarint(dst, uint64(n))
		if v.Type().Elem().Kind() == reflect.Uint8 {
			for i := 0; i < n; i++ {
				dst = append(dst, byte(v.Index(i).Uint()))
			}
			return dst
		}
		for i := 0; i < n; i++ {
			dst = appendEncodeVarint(dst, v.Index(i))
		}
		return dst

	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).PkgPath != "" {
				continue
			}
			dst = appendEncodeVarint(dst, v.Field(i))
		}
		return dst

	case reflect.Map:
		if v.IsNil() {
			return appendUvarint(dst, 0)
		}
		dst = appendUvarint(dst, uint64(v.Len()))
		iter := v.MapRange()
		for iter.Next() {
			dst = appendEncodeVarint(dst, iter.Key())
			dst = appendEncodeVarint(dst, iter.Value())
		}
		return dst

	case reflect.Ptr:
		return append(dst, encodeMsgpackChunk(msgpackChunkFromReflect(v))...)

	default:
		return append(dst, encodeMsgpackChunk(msgpackChunkFromReflect(v))...)
	}
}

func readUvarint(data []byte, off *int) (uint64, error) {
	if *off >= len(data) {
		return 0, io.ErrUnexpectedEOF
	}
	x, n := binary.Uvarint(data[*off:])
	if n <= 0 {
		return 0, fmt.Errorf("invalid uvarint at %d", *off)
	}
	*off += n
	return x, nil
}

func zigzagDecode(u uint64) int64 {
	return int64(u>>1) ^ int64(-(int64(u & 1)))
}

func readZigzagInt64(data []byte, off *int) (int64, error) {
	u, err := readUvarint(data, off)
	if err != nil {
		return 0, err
	}
	return zigzagDecode(u), nil
}

func decodeMsgpackSubtree(data []byte, off *int, rv reflect.Value) error {
	if !rv.CanAddr() {
		return fmt.Errorf("decode target not addressable")
	}
	dec := codec.NewDecoderBytes(data[*off:], CodecHandle)
	ptr := rv.Addr().Interface()
	if err := dec.Decode(ptr); err != nil {
		return err
	}
	*off += dec.NumBytesRead()
	return nil
}

func decodeVarintValue(data []byte, off *int, rv reflect.Value) error {
	for rv.Kind() == reflect.Interface && !rv.IsNil() {
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Bool:
		if *off >= len(data) {
			return io.ErrUnexpectedEOF
		}
		b := data[*off]
		*off++
		switch b {
		case 0:
			rv.SetBool(false)
		case 1:
			rv.SetBool(true)
		default:
			return fmt.Errorf("invalid bool encoding %d", b)
		}
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		x, err := readZigzagInt64(data, off)
		if err != nil {
			return err
		}
		if rv.OverflowInt(x) {
			return fmt.Errorf("int overflow")
		}
		rv.SetInt(x)
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		x, err := readUvarint(data, off)
		if err != nil {
			return err
		}
		if rv.OverflowUint(x) {
			return fmt.Errorf("uint overflow")
		}
		rv.SetUint(x)
		return nil

	case reflect.Float32:
		if *off+4 > len(data) {
			return io.ErrUnexpectedEOF
		}
		b := binary.LittleEndian.Uint32(data[*off:])
		*off += 4
		rv.SetFloat(float64(math.Float32frombits(b)))
		return nil

	case reflect.Float64:
		if *off+8 > len(data) {
			return io.ErrUnexpectedEOF
		}
		b := binary.LittleEndian.Uint64(data[*off:])
		*off += 8
		rv.SetFloat(math.Float64frombits(b))
		return nil

	case reflect.String:
		n, err := readUvarint(data, off)
		if err != nil {
			return err
		}
		if n > uint64(len(data)-*off) {
			return io.ErrUnexpectedEOF
		}
		ln := int(n)
		s := string(data[*off : *off+ln])
		*off += ln
		rv.SetString(s)
		return nil

	case reflect.Slice:
		n64, err := readUvarint(data, off)
		if err != nil {
			return err
		}
		if n64 > uint64(^uint(0)>>1) {
			return fmt.Errorf("slice length too large")
		}
		n := int(n64)
		et := rv.Type().Elem()
		if et.Kind() == reflect.Uint8 {
			if n > len(data)-*off {
				return io.ErrUnexpectedEOF
			}
			buf := make([]byte, n)
			copy(buf, data[*off:*off+n])
			*off += n
			rv.SetBytes(buf)
			return nil
		}
		ns := reflect.MakeSlice(rv.Type(), n, n)
		for i := 0; i < n; i++ {
			if err := decodeVarintValue(data, off, ns.Index(i)); err != nil {
				return err
			}
		}
		rv.Set(ns)
		return nil

	case reflect.Array:
		n64, err := readUvarint(data, off)
		if err != nil {
			return err
		}
		if int(n64) != rv.Len() {
			return fmt.Errorf("array length mismatch got %d want %d", n64, rv.Len())
		}
		n := rv.Len()
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			if n > len(data)-*off {
				return io.ErrUnexpectedEOF
			}
			for i := 0; i < n; i++ {
				rv.Index(i).SetUint(uint64(data[*off+i]))
			}
			*off += n
			return nil
		}
		for i := 0; i < n; i++ {
			if err := decodeVarintValue(data, off, rv.Index(i)); err != nil {
				return err
			}
		}
		return nil

	case reflect.Struct:
		t := rv.Type()
		for i := 0; i < rv.NumField(); i++ {
			if t.Field(i).PkgPath != "" {
				continue
			}
			if err := decodeVarintValue(data, off, rv.Field(i)); err != nil {
				return err
			}
		}
		return nil

	case reflect.Map:
		n64, err := readUvarint(data, off)
		if err != nil {
			return err
		}
		if n64 > uint64(^uint(0)>>1) {
			return fmt.Errorf("map length too large")
		}
		n := int(n64)
		if rv.Type().Kind() != reflect.Map {
			return fmt.Errorf("decode map into non-map")
		}
		mt := rv.Type()
		if rv.IsNil() {
			rv.Set(reflect.MakeMap(mt))
		}
		for i := 0; i < n; i++ {
			k := reflect.New(mt.Key()).Elem()
			ve := reflect.New(mt.Elem()).Elem()
			if err := decodeVarintValue(data, off, k); err != nil {
				return err
			}
			if err := decodeVarintValue(data, off, ve); err != nil {
				return err
			}
			rv.SetMapIndex(k, ve)
		}
		return nil

	case reflect.Ptr:
		return decodeMsgpackSubtree(data, off, rv)

	default:
		return decodeMsgpackSubtree(data, off, rv)
	}
}

func VarintScalarWireSize(v interface{}) int64 {
	if v == nil {
		return 0
	}
	val := reflect.ValueOf(v)
	val, ok := stripIfaceAndPtrChain(val)
	if !ok || !val.IsValid() {
		return 0
	}
	return int64(sizeValueVarint(val))
}

func uvarintLen(x uint64) int {
	n := 1
	for x >= 0x80 {
		x >>= 7
		n++
	}
	return n
}

func zigzagUint64(n int64) uint64 {
	return uint64((n << 1) ^ (n >> 63))
}

func sizeInt64Varint(n int64) int {
	return uvarintLen(zigzagUint64(n))
}

func sizeValueVarint(v reflect.Value) int {
	for v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Bool:
		return 1

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return sizeInt64Varint(v.Int())

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return uvarintLen(v.Uint())

	case reflect.Float32:
		return 4
	case reflect.Float64:
		return 8

	case reflect.String:
		s := v.String()
		return uvarintLen(uint64(len(s))) + len(s)

	case reflect.Slice, reflect.Array:
		n := v.Len()
		sum := uvarintLen(uint64(n))
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return sum + n
		}
		for i := 0; i < n; i++ {
			sum += sizeValueVarint(v.Index(i))
		}
		return sum

	case reflect.Struct:
		t := v.Type()
		sum := 0
		for i := 0; i < v.NumField(); i++ {
			if t.Field(i).PkgPath != "" {
				continue
			}
			sum += sizeValueVarint(v.Field(i))
		}
		return sum

	case reflect.Map:
		if v.IsNil() {
			return uvarintLen(0)
		}
		sum := uvarintLen(uint64(v.Len()))
		iter := v.MapRange()
		for iter.Next() {
			sum += sizeValueVarint(iter.Key())
			sum += sizeValueVarint(iter.Value())
		}
		return sum

	case reflect.Ptr:
		return len(encodeMsgpackChunk(msgpackChunkFromReflect(v)))

	default:
		return len(encodeMsgpackChunk(msgpackChunkFromReflect(v)))
	}
}

func VarintEncodedLenUint64(x uint64) int {
	return uvarintLen(x)
}

func VarintEncodedLenInt64(x int64) int {
	return uvarintLen(zigzagUint64(x))
}

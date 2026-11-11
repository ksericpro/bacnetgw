package bacnet

import (
	"encoding/binary"
	"fmt"
	"math"
	"unicode/utf16"
)

// Application Tag Numbers (ASHRAE-135 Clause 20.2.1.4)
const (
	AppTagNull             uint8 = 0
	AppTagBoolean          uint8 = 1
	AppTagUnsignedInt      uint8 = 2
	AppTagSignedInt        uint8 = 3
	AppTagReal             uint8 = 4
	AppTagDouble           uint8 = 5
	AppTagOctetString      uint8 = 6
	AppTagCharacterString  uint8 = 7
	AppTagBitString        uint8 = 8
	AppTagEnumerated       uint8 = 9
	AppTagDate             uint8 = 10
	AppTagTime             uint8 = 11
	AppTagObjectIdentifier uint8 = 12
)

// TagHeader represents a parsed BACnet tag header.
type TagHeader struct {
	TagNumber uint8
	IsContext bool
	IsOpening bool
	IsClosing bool
	Length    uint32 // Value bit for Application Boolean, or byte count for all other data tags
}

// ReadTag parses a single BACnet tag header starting at buf[offset] and returns the header and new offset.
func ReadTag(buf []byte, offset int) (TagHeader, int, error) {
	if offset >= len(buf) {
		return TagHeader{}, offset, fmt.Errorf("unexpected EOF reading tag at offset %d", offset)
	}
	b := buf[offset]
	offset++

	var hdr TagHeader
	hdr.TagNumber = (b >> 4) & 0x0F
	hdr.IsContext = (b & 0x08) != 0
	lvt := b & 0x07

	// Extended tag number (tag == 15)
	if hdr.TagNumber == 0x0F {
		if offset >= len(buf) {
			return TagHeader{}, offset, fmt.Errorf("unexpected EOF reading extended tag number")
		}
		hdr.TagNumber = buf[offset]
		offset++
	}

	// Opening / Closing tags only apply to context-specific tags
	if hdr.IsContext && lvt == 6 {
		hdr.IsOpening = true
		return hdr, offset, nil
	}
	if hdr.IsContext && lvt == 7 {
		hdr.IsClosing = true
		return hdr, offset, nil
	}

	// Application Boolean encodes boolean value in lvt (0 or 1) with 0 content bytes
	if !hdr.IsContext && hdr.TagNumber == AppTagBoolean {
		hdr.Length = uint32(lvt)
		return hdr, offset, nil
	}

	// Extended length encoding
	switch lvt {
	case 5:
		if offset >= len(buf) {
			return TagHeader{}, offset, fmt.Errorf("unexpected EOF reading 8-bit extended length")
		}
		ext := buf[offset]
		offset++
		switch ext {
		case 254:
			if offset+2 > len(buf) {
				return TagHeader{}, offset, fmt.Errorf("unexpected EOF reading 16-bit extended length")
			}
			hdr.Length = uint32(binary.BigEndian.Uint16(buf[offset : offset+2]))
			offset += 2
		case 255:
			if offset+4 > len(buf) {
				return TagHeader{}, offset, fmt.Errorf("unexpected EOF reading 32-bit extended length")
			}
			hdr.Length = binary.BigEndian.Uint32(buf[offset : offset+4])
			offset += 4
		default:
			hdr.Length = uint32(ext)
		}
	default:
		hdr.Length = uint32(lvt)
	}

	return hdr, offset, nil
}

// WriteTagHeader appends a BACnet tag header to dst.
func WriteTagHeader(dst []byte, tagNum uint8, isContext bool, length uint32) []byte {
	var first byte
	var extTag bool
	if tagNum < 15 {
		first = tagNum << 4
	} else {
		first = 0xF0
		extTag = true
	}
	if isContext {
		first |= 0x08
	}

	if length < 5 {
		first |= byte(length)
		dst = append(dst, first)
		if extTag {
			dst = append(dst, tagNum)
		}
		return dst
	}

	first |= 0x05 // extended length indicator
	dst = append(dst, first)
	if extTag {
		dst = append(dst, tagNum)
	}
	if length < 254 {
		dst = append(dst, byte(length))
	} else if length <= 0xFFFF {
		dst = append(dst, 254, byte(length>>8), byte(length))
	} else {
		dst = append(dst, 255, byte(length>>24), byte(length>>16), byte(length>>8), byte(length))
	}
	return dst
}

// WriteOpeningTag appends a context opening tag [tagNum].
func WriteOpeningTag(dst []byte, tagNum uint8) []byte {
	if tagNum < 15 {
		return append(dst, (tagNum<<4)|0x08|0x06)
	}
	return append(dst, 0xFE, tagNum)
}

// WriteClosingTag appends a context closing tag [tagNum].
func WriteClosingTag(dst []byte, tagNum uint8) []byte {
	if tagNum < 15 {
		return append(dst, (tagNum<<4)|0x08|0x07)
	}
	return append(dst, 0xFF, tagNum)
}

// EncodeUnsignedBytes encodes a uint64 in minimum big-endian bytes (1..8).
func EncodeUnsignedBytes(v uint64) []byte {
	switch {
	case v <= 0xFF:
		return []byte{byte(v)}
	case v <= 0xFFFF:
		return []byte{byte(v >> 8), byte(v)}
	case v <= 0xFFFFFF:
		return []byte{byte(v >> 16), byte(v >> 8), byte(v)}
	case v <= 0xFFFFFFFF:
		return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
	default:
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], v)
		return b[:]
	}
}

// DecodeUnsigned parses a 1..8 byte big-endian unsigned integer.
func DecodeUnsigned(b []byte) uint64 {
	var v uint64
	for _, x := range b {
		v = (v << 8) | uint64(x)
	}
	return v
}

// DecodeSigned parses a 1..8 byte big-endian two's-complement signed integer.
func DecodeSigned(b []byte) int64 {
	if len(b) == 0 {
		return 0
	}
	var u uint64
	if b[0]&0x80 != 0 {
		u = ^uint64(0)
	}
	for _, x := range b {
		u = (u << 8) | uint64(x)
	}
	return int64(u)
}

// EncodeObjectIDBytes encodes an ObjectID into 4 big-endian bytes.
func EncodeObjectIDBytes(id ObjectID) [4]byte {
	raw := (uint32(id.Type)&0x3FF)<<22 | (id.Instance & 0x3FFFFF)
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], raw)
	return b
}

// DecodeObjectIDBytes decodes 4 bytes into an ObjectID.
func DecodeObjectIDBytes(b []byte) (ObjectID, error) {
	if len(b) != 4 {
		return ObjectID{}, fmt.Errorf("invalid ObjectID length %d (want 4)", len(b))
	}
	raw := binary.BigEndian.Uint32(b)
	return ObjectID{
		Type:     ObjectType((raw >> 22) & 0x3FF),
		Instance: raw & 0x3FFFFF,
	}, nil
}

// AppendContextObjectID appends a context-tagged BACnetObjectIdentifier.
func AppendContextObjectID(dst []byte, tagNum uint8, id ObjectID) []byte {
	raw := EncodeObjectIDBytes(id)
	dst = WriteTagHeader(dst, tagNum, true, 4)
	return append(dst, raw[:]...)
}

// AppendContextUnsigned appends a context-tagged Unsigned integer.
func AppendContextUnsigned(dst []byte, tagNum uint8, v uint64) []byte {
	enc := EncodeUnsignedBytes(v)
	dst = WriteTagHeader(dst, tagNum, true, uint32(len(enc)))
	return append(dst, enc...)
}

// AppendContextBool appends a context-tagged Boolean (1 content byte per ASHRAE-135 20.2.3).
func AppendContextBool(dst []byte, tagNum uint8, v bool) []byte {
	dst = WriteTagHeader(dst, tagNum, true, 1)
	if v {
		return append(dst, 0x01)
	}
	return append(dst, 0x00)
}

// AppendAppObjectID appends an application-tagged BACnetObjectIdentifier.
func AppendAppObjectID(dst []byte, id ObjectID) []byte {
	raw := EncodeObjectIDBytes(id)
	dst = WriteTagHeader(dst, AppTagObjectIdentifier, false, 4)
	return append(dst, raw[:]...)
}

// AppendAppUnsigned appends an application-tagged Unsigned integer.
func AppendAppUnsigned(dst []byte, v uint64) []byte {
	enc := EncodeUnsignedBytes(v)
	dst = WriteTagHeader(dst, AppTagUnsignedInt, false, uint32(len(enc)))
	return append(dst, enc...)
}

// AppendAppEnumerated appends an application-tagged Enumerated value.
func AppendAppEnumerated(dst []byte, v uint32) []byte {
	enc := EncodeUnsignedBytes(uint64(v))
	dst = WriteTagHeader(dst, AppTagEnumerated, false, uint32(len(enc)))
	return append(dst, enc...)
}

// AppendAppReal appends an application-tagged float32 Real value.
func AppendAppReal(dst []byte, v float32) []byte {
	dst = WriteTagHeader(dst, AppTagReal, false, 4)
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], math.Float32bits(v))
	return append(dst, b[:]...)
}

// AppendAppDouble appends an application-tagged float64 Double value.
func AppendAppDouble(dst []byte, v float64) []byte {
	dst = WriteTagHeader(dst, AppTagDouble, false, 8)
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], math.Float64bits(v))
	return append(dst, b[:]...)
}

// AppendAppBool appends an application-tagged Boolean.
func AppendAppBool(dst []byte, v bool) []byte {
	if v {
		return append(dst, 0x11)
	}
	return append(dst, 0x10)
}

// AppendAppCharString appends an application-tagged UTF-8 CharacterString.
func AppendAppCharString(dst []byte, s string) []byte {
	strBytes := []byte(s)
	dst = WriteTagHeader(dst, AppTagCharacterString, false, uint32(len(strBytes)+1))
	dst = append(dst, 0x00) // 0x00 = UTF-8 / ANSI X3.4
	return append(dst, strBytes...)
}

// AppendAppStatusFlags appends a 4-bit BACnet Status_Flags bitstring.
func AppendAppStatusFlags(dst []byte, sf StatusFlags) []byte {
	dst = WriteTagHeader(dst, AppTagBitString, false, 2)
	var b byte
	// Bit 0 (MSB 0x80) = InAlarm, Bit 1 (0x40) = Fault, Bit 2 (0x20) = Overridden, Bit 3 (0x10) = OutOfService
	if sf.InAlarm {
		b |= 0x80
	}
	if sf.Fault {
		b |= 0x40
	}
	if sf.Overridden {
		b |= 0x20
	}
	if sf.OutOfService {
		b |= 0x10
	}
	return append(dst, 0x04, b) // 4 unused bits in last octet
}

// DecodeCharacterString decodes a BACnet CharacterString payload (encoding byte + content).
func DecodeCharacterString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	encoding := b[0]
	payload := b[1:]
	switch encoding {
	case 0: // UTF-8
		return string(payload)
	case 4: // UCS-2 / UTF-16BE
		if len(payload)%2 != 0 {
			payload = payload[:len(payload)-1]
		}
		u16 := make([]uint16, len(payload)/2)
		for i := range u16 {
			u16[i] = binary.BigEndian.Uint16(payload[i*2 : i*2+2])
		}
		return string(utf16.Decode(u16))
	case 5: // ISO 8859-1 (Latin-1)
		runes := make([]rune, len(payload))
		for i, c := range payload {
			runes[i] = rune(c)
		}
		return string(runes)
	default:
		return string(payload)
	}
}

// DecodeBitStringStatusFlags decodes a BACnet BitString payload into StatusFlags or []bool.
func DecodeBitString(b []byte) []bool {
	if len(b) < 2 {
		return nil
	}
	unused := int(b[0])
	data := b[1:]
	totalBits := len(data)*8 - unused
	if totalBits <= 0 {
		return nil
	}
	bits := make([]bool, 0, totalBits)
	for i := 0; i < totalBits; i++ {
		byteIdx := i / 8
		bitIdx := 7 - (i % 8)
		bits = append(bits, (data[byteIdx]&(1<<bitIdx)) != 0)
	}
	return bits
}

// BitsToStatusFlags converts a decoded BitString into StatusFlags.
func BitsToStatusFlags(bits []bool) StatusFlags {
	var sf StatusFlags
	if len(bits) > 0 {
		sf.InAlarm = bits[0]
	}
	if len(bits) > 1 {
		sf.Fault = bits[1]
	}
	if len(bits) > 2 {
		sf.Overridden = bits[2]
	}
	if len(bits) > 3 {
		sf.OutOfService = bits[3]
	}
	return sf
}

// DecodeApplicationValue reads one application-tagged value (or skips/returns nested constructed block) from buf[offset].
func DecodeApplicationValue(buf []byte, offset int) (any, int, error) {
	hdr, next, err := ReadTag(buf, offset)
	if err != nil {
		return nil, offset, err
	}
	if hdr.IsContext {
		if hdr.IsOpening {
			// Skip or collect constructed context value up to matching closing tag
			var items []any
			cur := next
			for cur < len(buf) {
				peekHdr, peekNext, err := ReadTag(buf, cur)
				if err != nil {
					return nil, cur, err
				}
				if peekHdr.IsContext && peekHdr.IsClosing && peekHdr.TagNumber == hdr.TagNumber {
					return items, peekNext, nil
				}
				val, afterVal, err := DecodeApplicationValue(buf, cur)
				if err != nil {
					return nil, cur, err
				}
				items = append(items, val)
				cur = afterVal
			}
			return nil, cur, fmt.Errorf("unclosed context tag %d", hdr.TagNumber)
		}
		if hdr.IsClosing {
			return nil, next, fmt.Errorf("unexpected closing tag %d", hdr.TagNumber)
		}
		end := next + int(hdr.Length)
		if end > len(buf) {
			return nil, next, fmt.Errorf("context tag %d length %d exceeds buffer", hdr.TagNumber, hdr.Length)
		}
		cp := make([]byte, hdr.Length)
		copy(cp, buf[next:end])
		return cp, end, nil
	}

	if hdr.TagNumber == AppTagBoolean {
		return hdr.Length != 0, next, nil
	}

	end := next + int(hdr.Length)
	if end > len(buf) {
		return nil, next, fmt.Errorf("app tag %d length %d exceeds buffer (%d > %d)", hdr.TagNumber, hdr.Length, end, len(buf))
	}
	payload := buf[next:end]

	switch hdr.TagNumber {
	case AppTagNull:
		return nil, end, nil
	case AppTagUnsignedInt:
		return DecodeUnsigned(payload), end, nil
	case AppTagSignedInt:
		return DecodeSigned(payload), end, nil
	case AppTagReal:
		if len(payload) != 4 {
			return nil, end, fmt.Errorf("invalid Real length %d", len(payload))
		}
		f := math.Float32frombits(binary.BigEndian.Uint32(payload))
		return float64(f), end, nil
	case AppTagDouble:
		if len(payload) != 8 {
			return nil, end, fmt.Errorf("invalid Double length %d", len(payload))
		}
		return math.Float64frombits(binary.BigEndian.Uint64(payload)), end, nil
	case AppTagOctetString:
		cp := make([]byte, len(payload))
		copy(cp, payload)
		return cp, end, nil
	case AppTagCharacterString:
		return DecodeCharacterString(payload), end, nil
	case AppTagBitString:
		bits := DecodeBitString(payload)
		if len(bits) == 4 {
			return BitsToStatusFlags(bits), end, nil
		}
		return bits, end, nil
	case AppTagEnumerated:
		return uint32(DecodeUnsigned(payload)), end, nil
	case AppTagDate:
		if len(payload) == 4 {
			return fmt.Sprintf("%04d-%02d-%02d", 1900+int(payload[0]), payload[1], payload[2]), end, nil
		}
		return payload, end, nil
	case AppTagTime:
		if len(payload) == 4 {
			return fmt.Sprintf("%02d:%02d:%02d.%02d", payload[0], payload[1], payload[2], payload[3]), end, nil
		}
		return payload, end, nil
	case AppTagObjectIdentifier:
		obj, err := DecodeObjectIDBytes(payload)
		return obj, end, err
	default:
		cp := make([]byte, len(payload))
		copy(cp, payload)
		return cp, end, nil
	}
}

// DecodeValuesUntilClosing reads application-tagged values starting at offset until closing tag [closingTagNum] is reached.
func DecodeValuesUntilClosing(buf []byte, offset int, closingTagNum uint8) ([]any, int, error) {
	var vals []any
	cur := offset
	for cur < len(buf) {
		hdr, afterHdr, err := ReadTag(buf, cur)
		if err != nil {
			return nil, cur, err
		}
		if hdr.IsContext && hdr.IsClosing && hdr.TagNumber == closingTagNum {
			return vals, afterHdr, nil
		}
		v, afterVal, err := DecodeApplicationValue(buf, cur)
		if err != nil {
			return nil, cur, err
		}
		vals = append(vals, v)
		cur = afterVal
	}
	return nil, cur, fmt.Errorf("missing closing tag [%d]", closingTagNum)
}


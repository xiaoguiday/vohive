package simauth

import (
	"fmt"
)

type TLV struct {
	Tag   int
	Value []byte
}

func ParseTLVList(data []byte) ([]TLV, error) {
	var out []TLV
	for len(data) > 0 {
		data = trimTLVPadding(data)
		if len(data) == 0 {
			break
		}
		tag, rest, err := readTag(data)
		if err != nil {
			return out, err
		}
		length, rest, err := readLength(rest)
		if err != nil {
			return out, err
		}
		if length > len(rest) {
			return out, fmt.Errorf("TLV tag 0x%X length %d exceeds remaining %d", tag, length, len(rest))
		}
		out = append(out, TLV{Tag: tag, Value: append([]byte(nil), rest[:length]...)})
		data = rest[length:]
	}
	return out, nil
}

func trimTLVPadding(data []byte) []byte {
	for len(data) > 0 && (data[0] == 0x00 || data[0] == 0xFF) {
		data = data[1:]
	}
	return data
}

func FindTLV(data []byte, tag int) ([]byte, bool) {
	items, err := ParseTLVList(data)
	if err != nil {
		return nil, false
	}
	for _, item := range items {
		if item.Tag == tag {
			return item.Value, true
		}
		if isConstructed(item.Tag) {
			if v, ok := FindTLV(item.Value, tag); ok {
				return v, true
			}
		}
	}
	return nil, false
}

func FileSizeFromFCP(fcp []byte) int {
	body := fcp
	if v, ok := FindTLV(fcp, 0x62); ok {
		body = v
	}
	if v, ok := FindTLV(body, 0x80); ok {
		return beInt(v)
	}
	if v, ok := FindTLV(body, 0x81); ok {
		return beInt(v)
	}
	return 0
}

func RecordInfoFromFCP(fcp []byte) (recordLength int, recordCount int) {
	body := fcp
	if v, ok := FindTLV(fcp, 0x62); ok {
		body = v
	}
	if v, ok := FindTLV(body, 0x82); ok && len(v) >= 5 {
		recordLength = int(v[2])<<8 | int(v[3])
		recordCount = int(v[4])
	}
	return recordLength, recordCount
}

func readTag(data []byte) (int, []byte, error) {
	if len(data) == 0 {
		return 0, nil, fmt.Errorf("empty TLV tag")
	}
	tag := int(data[0])
	data = data[1:]
	if (tag & 0x1F) == 0x1F {
		tag = tag << 8
		for {
			if len(data) == 0 {
				return 0, nil, fmt.Errorf("truncated high-tag-number TLV")
			}
			b := data[0]
			data = data[1:]
			tag |= int(b)
			if b&0x80 == 0 {
				break
			}
			tag <<= 8
		}
	}
	return tag, data, nil
}

func readLength(data []byte) (int, []byte, error) {
	if len(data) == 0 {
		return 0, nil, fmt.Errorf("empty TLV length")
	}
	b := data[0]
	data = data[1:]
	if b&0x80 == 0 {
		return int(b), data, nil
	}
	n := int(b & 0x7F)
	if n == 0 || n > 3 {
		return 0, nil, fmt.Errorf("unsupported TLV length form 0x%02X", b)
	}
	if len(data) < n {
		return 0, nil, fmt.Errorf("truncated TLV long length")
	}
	length := 0
	for _, part := range data[:n] {
		length = (length << 8) | int(part)
	}
	return length, data[n:], nil
}

func isConstructed(tag int) bool {
	if tag <= 0xFF {
		return tag&0x20 != 0
	}
	for tag > 0xFF {
		tag >>= 8
	}
	return tag&0x20 != 0
}

func beInt(v []byte) int {
	out := 0
	for _, b := range v {
		out = (out << 8) | int(b)
	}
	return out
}

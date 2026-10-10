package sberapi

import (
	"errors"
	"fmt"
)

// berToDER переписывает BER-структуру в DER: заменяет неопределённую длину (0x80 ... 00 00)
// на определённую. Контейнеры .p12 из кабинета Сбера (Java) приходят именно так, а
// go-pkcs12 читает только DER. Примитивные значения (в том числе зашифрованное содержимое)
// не затрагиваются.
func berToDER(b []byte) ([]byte, error) {
	out, rest, err := berElement(b, 0)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, errors.New("trailing data after the structure")
	}
	return out, nil
}

// berElement разбирает один элемент и возвращает его в DER и остаток входа.
func berElement(b []byte, depth int) (der, rest []byte, err error) {
	if depth > 32 {
		return nil, nil, errors.New("structure is nested too deep")
	}
	if len(b) < 2 {
		return nil, nil, errors.New("unexpected end of data")
	}
	i := 1
	if b[0]&0x1f == 0x1f { // тег с номером в нескольких байтах
		for i < len(b) && b[i]&0x80 != 0 {
			i++
		}
		i++
	}
	if i >= len(b) {
		return nil, nil, errors.New("unexpected end of data")
	}
	tag := b[:i]
	constructed := b[0]&0x20 != 0
	lb := b[i]
	i++

	var content []byte
	switch {
	case lb == 0x80: // неопределённая длина: дети до 00 00
		if !constructed {
			return nil, nil, errors.New("indefinite length in a primitive element")
		}
		rest = b[i:]
		for {
			if len(rest) >= 2 && rest[0] == 0 && rest[1] == 0 {
				rest = rest[2:]
				break
			}
			var child []byte
			child, rest, err = berElement(rest, depth+1)
			if err != nil {
				return nil, nil, err
			}
			content = append(content, child...)
		}
	default:
		n := int(lb)
		if lb&0x80 != 0 {
			k := int(lb & 0x7f)
			if k > 4 || i+k > len(b) {
				return nil, nil, fmt.Errorf("bad length encoding")
			}
			n = 0
			for _, c := range b[i : i+k] {
				n = n<<8 | int(c)
			}
			i += k
		}
		if n < 0 || i+n > len(b) {
			return nil, nil, errors.New("length exceeds data")
		}
		content, rest = b[i:i+n], b[i+n:]
		if constructed {
			var conv []byte
			for r := content; len(r) > 0; {
				var child []byte
				child, r, err = berElement(r, depth+1)
				if err != nil {
					return nil, nil, err
				}
				conv = append(conv, child...)
			}
			content = conv
		}
	}
	der = append(append(append([]byte{}, tag...), derLen(len(content))...), content...)
	return der, rest, nil
}

func derLen(n int) []byte {
	if n < 0x80 {
		return []byte{byte(n)}
	}
	var l []byte
	for ; n > 0; n >>= 8 {
		l = append([]byte{byte(n)}, l...)
	}
	return append([]byte{0x80 | byte(len(l))}, l...)
}

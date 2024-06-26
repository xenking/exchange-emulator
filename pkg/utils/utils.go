package utils

import (
	"errors"
	"math/big"
	"strings"
	"sync"

	"github.com/xenking/decimal"
)

func WriteUint(n int64) string {
	var b [20]byte
	buf := b[:]
	i := len(buf)
	var q int64
	for n >= 10 {
		i--
		q = n / 10
		buf[i] = '0' + byte(n-q*10)
		n = q
	}
	i--
	buf[i] = '0' + byte(n)

	var s strings.Builder
	s.Write(buf[i:])

	return s.String()
}

func AppendUint(dst []byte, n int64) []byte {
	var b [20]byte
	buf := b[:]
	i := len(buf)
	var q int64
	for n >= 10 {
		i--
		q = n / 10
		buf[i] = '0' + byte(n-q*10)
		n = q
	}
	i--
	buf[i] = '0' + byte(n)
	return append(dst, buf[i:]...)
}

func AppendDecimal(dst []byte, v decimal.Decimal) []byte {
	var b [21]byte
	buf := b[:]
	i := len(buf)
	n := v.CoefficientInt64()
	exp := int(v.Exponent())
	var q int64
	for n >= 10 {
		if exp < 0 && i == cap(b)+exp {
			i--
			buf[i] = '.'
		}
		i--
		q = n / 10
		buf[i] = '0' + byte(n-q*10)
		n = q
	}
	i--
	buf[i] = '0' + byte(n)

	dst = append(dst, buf[i:]...)
	if exp > 0 {
		dst = append(dst, '0')
	}
	return dst
}

// ParseUint parses uint from buf.
func ParseUint(buf string) (int64, error) {
	v, n, err := parseUintBuf(buf)
	if n != len(buf) {
		return -1, errUnexpectedTrailingChar
	}

	return v, err
}

var (
	errEmptyInt               = errors.New("empty integer")
	errUnexpectedFirstChar    = errors.New("unexpected first char found. Expecting 0-9")
	errUnexpectedTrailingChar = errors.New("unexpected trailing char found. Expecting 0-9")
	errTooLongInt             = errors.New("too long int")
)

func parseUintBuf(b string) (value int64, n int, err error) {
	n = len(b)
	if n == 0 {
		return -1, 0, errEmptyInt
	}
	value = 0
	for i := 0; i < n; i++ {
		c := b[i]
		k := c - '0'
		if k > 9 {
			if i == 0 {
				return -1, i, errUnexpectedFirstChar
			}

			return value, i, nil
		}
		vNew := 10*value + int64(k)
		// Test for overflow.
		if vNew < value {
			return -1, i, errTooLongInt
		}
		value = vNew
	}

	return value, n, nil
}

// ParseUintBytes parses uint from buf.
func ParseUintBytes(buf []byte) (int64, error) {
	v, n, err := parseUintBufBytes(buf)
	if n != len(buf) {
		return -1, errUnexpectedTrailingChar
	}

	return v, err
}

func parseUintBufBytes(b []byte) (value int64, n int, err error) {
	n = len(b)
	if n == 0 {
		return -1, 0, errEmptyInt
	}
	value = 0
	for i := 0; i < n; i++ {
		c := b[i]
		k := c - '0'
		if k > 9 {
			if i == 0 {
				return -1, i, errUnexpectedFirstChar
			}

			return value, i, nil
		}
		vNew := 10*value + int64(k)
		// Test for overflow.
		if vNew < value {
			return -1, i, errTooLongInt
		}
		value = vNew
	}

	return value, n, nil
}

var bigIntPool = sync.Pool{
	New: func() interface{} {
		return new(big.Int)
	},
}

func ParseDecimalBytes(b []byte) (value decimal.Decimal, err error) {
	n := len(b)
	if n == 0 {
		return decimal.Zero, errEmptyInt
	}
	num := uint64(0)
	exp := 0
	for i := 0; i < n; i++ {
		c := b[i]
		if c == '.' {
			exp = i + 1 - len(b)
			continue
		}
		k := c - '0'
		if k > 9 {
			if i == 0 {
				return decimal.Zero, errUnexpectedFirstChar
			}

			return value, nil
		}
		vNew := 10*num + uint64(k)
		num = vNew
	}

	bi := bigIntPool.Get()
	bigInt := bi.(*big.Int)
	bigInt.SetUint64(num)
	value = decimal.NewFromBigInt(bigInt, int32(exp))
	bigIntPool.Put(bi)

	return value, nil
}

const toLowerTable = "\x00\x01\x02\x03\x04\x05\x06\a\b\t\n\v\f\r\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f !\"#$%&'()*+,-./0123456789:;<=>?@abcdefghijklmnopqrstuvwxyz[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~\u007f\x80\x81\x82\x83\x84\x85\x86\x87\x88\x89\x8a\x8b\x8c\x8d\x8e\x8f\x90\x91\x92\x93\x94\x95\x96\x97\x98\x99\x9a\x9b\x9c\x9d\x9e\x9f\xa0\xa1\xa2\xa3\xa4\xa5\xa6\xa7\xa8\xa9\xaa\xab\xac\xad\xae\xaf\xb0\xb1\xb2\xb3\xb4\xb5\xb6\xb7\xb8\xb9\xba\xbb\xbc\xbd\xbe\xbf\xc0\xc1\xc2\xc3\xc4\xc5\xc6\xc7\xc8\xc9\xca\xcb\xcc\xcd\xce\xcf\xd0\xd1\xd2\xd3\xd4\xd5\xd6\xd7\xd8\xd9\xda\xdb\xdc\xdd\xde\xdf\xe0\xe1\xe2\xe3\xe4\xe5\xe6\xe7\xe8\xe9\xea\xeb\xec\xed\xee\xef\xf0\xf1\xf2\xf3\xf4\xf5\xf6\xf7\xf8\xf9\xfa\xfb\xfc\xfd\xfe\xff"

func ToLower(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		b.WriteByte(toLowerTable[s[i]])
	}

	return b.String()
}

func ToLowerTrim(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '_' {
			continue
		}
		b.WriteByte(toLowerTable[s[i]])
	}

	return b.String()
}

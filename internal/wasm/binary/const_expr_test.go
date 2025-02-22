package binary

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/tetratelabs/wazero/internal/testing/require"
	"github.com/tetratelabs/wazero/internal/wasm"
)

func TestDecodeConstantExpression(t *testing.T) {
	for i, tc := range []struct {
		in  []byte
		exp *wasm.ConstantExpression
	}{
		{
			in: []byte{
				wasm.OpcodeRefFunc,
				0x80, 0, // Multi byte zero.
				wasm.OpcodeEnd,
			},
			exp: &wasm.ConstantExpression{
				Opcode: wasm.OpcodeRefFunc,
				Data:   []byte{0x80, 0},
			},
		},
		{
			in: []byte{
				wasm.OpcodeRefFunc,
				0x80, 0x80, 0x80, 0x4f, // 165675008 in varint encoding.
				wasm.OpcodeEnd,
			},
			exp: &wasm.ConstantExpression{
				Opcode: wasm.OpcodeRefFunc,
				Data:   []byte{0x80, 0x80, 0x80, 0x4f},
			},
		},
		{
			in: []byte{
				wasm.OpcodeRefNull,
				wasm.RefTypeFuncref,
				wasm.OpcodeEnd,
			},
			exp: &wasm.ConstantExpression{
				Opcode: wasm.OpcodeRefNull,
				Data: []byte{
					wasm.RefTypeFuncref,
				},
			},
		},
	} {
		tc := tc
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			actual, err := decodeConstantExpression(bytes.NewReader(tc.in), wasm.FeatureBulkMemoryOperations)
			require.NoError(t, err)
			require.Equal(t, tc.exp, actual)
		})
	}
}

func TestDecodeConstantExpression_errors(t *testing.T) {
	for _, tc := range []struct {
		in          []byte
		expectedErr string
		features    wasm.Features
	}{
		{
			in: []byte{
				wasm.OpcodeRefFunc,
				0,
			},
			expectedErr: "look for end opcode: EOF",
			features:    wasm.FeatureBulkMemoryOperations,
		},
		{
			in: []byte{
				wasm.OpcodeRefNull,
				wasm.RefTypeExternref,
			},
			expectedErr: "ref.null instruction in constant expression must be of funcref type but was 0x6f",
			features:    wasm.FeatureBulkMemoryOperations,
		},
		{
			in: []byte{
				wasm.OpcodeRefNull,
				wasm.RefTypeExternref,
				wasm.OpcodeEnd,
			},
			expectedErr: "ref.null is not supported as feature \"bulk-memory-operations\" is disabled",
			features:    wasm.Features20191205,
		},
		{
			in: []byte{
				wasm.OpcodeRefFunc,
				0x80, 0,
				wasm.OpcodeEnd,
			},
			expectedErr: "ref.func is not supported as feature \"bulk-memory-operations\" is disabled",
			features:    wasm.Features20191205,
		},
	} {
		t.Run(tc.expectedErr, func(t *testing.T) {
			_, err := decodeConstantExpression(bytes.NewReader(tc.in), tc.features)
			require.EqualError(t, err, tc.expectedErr)
		})
	}
}

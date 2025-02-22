package interpreter

import (
	"testing"

	"github.com/tetratelabs/wazero/internal/integration_test/vs"
)

var runtime = vs.NewWazeroInterpreterRuntime

func TestAllocation(t *testing.T) {
	vs.RunTestAllocation(t, runtime)
}

func BenchmarkAllocation(b *testing.B) {
	vs.RunBenchmarkAllocation(b, runtime)
}

func TestBenchmarkAllocation_Call_JITFastest(t *testing.T) {
	vs.RunTestBenchmarkAllocation_Call_JITFastest(t, runtime())
}

func TestFactorial(t *testing.T) {
	vs.RunTestFactorial(t, runtime)
}

func BenchmarkFactorial(b *testing.B) {
	vs.RunBenchmarkFactorial(b, runtime)
}

func TestBenchmarkFactorial_Call_JITFastest(t *testing.T) {
	vs.RunTestBenchmarkFactorial_Call_JITFastest(t, runtime())
}

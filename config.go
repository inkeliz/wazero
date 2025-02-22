package wazero

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"strings"

	"github.com/tetratelabs/wazero/internal/wasm"
	"github.com/tetratelabs/wazero/internal/wasm/interpreter"
	"github.com/tetratelabs/wazero/internal/wasm/jit"
)

// RuntimeConfig controls runtime behavior, with the default implementation as NewRuntimeConfig
//
// Ex. To explicitly limit to Wasm Core 1.0 features as opposed to relying on defaults:
//	rConfig = wazero.NewRuntimeConfig().WithWasmCore1()
//
// Note: RuntimeConfig is immutable. Each WithXXX function returns a new instance including the corresponding change.
type RuntimeConfig interface {

	// WithFeatureBulkMemoryOperations adds instructions modify ranges of memory or table entries
	// ("bulk-memory-operations"). This defaults to false as the feature was not finished in WebAssembly 1.0.
	//
	// Here are the notable effects:
	// * Adds `memory.fill`, `memory.init`, `memory.copy` and `data.drop` instructions.
	// * Adds `table.init`, `table.copy` and `elem.drop` instructions.
	// * Introduces a "passive" form of element and data segments.
	// * Stops checking "active" element and data segment boundaries at compile-time, meaning they can error at runtime.
	//
	// Note: "bulk-memory-operations" is mixed with the "reference-types" proposal
	// due to the WebAssembly Working Group merging them "mutually dependent".
	// Therefore, enabling this feature results in enabling WithFeatureReferenceTypes, and vice-versa.
	// See https://github.com/WebAssembly/spec/blob/main/proposals/bulk-memory-operations/Overview.md
	// See https://github.com/WebAssembly/spec/blob/main/proposals/reference-types/Overview.md
	// See https://github.com/WebAssembly/spec/pull/1287
	WithFeatureBulkMemoryOperations(bool) RuntimeConfig

	// WithFeatureMultiValue enables multiple values ("multi-value"). This defaults to false as the feature was not
	// finished in WebAssembly 1.0 (20191205).
	//
	// Here are the notable effects:
	// * Function (`func`) types allow more than one result
	// * Block types (`block`, `loop` and `if`) can be arbitrary function types
	//
	// See https://github.com/WebAssembly/spec/blob/main/proposals/multi-value/Overview.md
	WithFeatureMultiValue(bool) RuntimeConfig

	// WithFeatureMutableGlobal allows globals to be mutable. This defaults to true as the feature was finished in
	// WebAssembly 1.0 (20191205).
	//
	// When false, an api.Global can never be cast to an api.MutableGlobal, and any source that includes global vars
	// will fail to parse.
	WithFeatureMutableGlobal(bool) RuntimeConfig

	// WithFeatureNonTrappingFloatToIntConversion enables non-trapping float-to-int conversions.
	// ("nontrapping-float-to-int-conversion"). This defaults to false as the feature was not in WebAssembly 1.0.
	//
	// The only effect of enabling is allowing the following instructions, which return 0 on NaN instead of panicking.
	// * `i32.trunc_sat_f32_s`
	// * `i32.trunc_sat_f32_u`
	// * `i32.trunc_sat_f64_s`
	// * `i32.trunc_sat_f64_u`
	// * `i64.trunc_sat_f32_s`
	// * `i64.trunc_sat_f32_u`
	// * `i64.trunc_sat_f64_s`
	// * `i64.trunc_sat_f64_u`
	//
	// See https://github.com/WebAssembly/spec/blob/main/proposals/nontrapping-float-to-int-conversion/Overview.md
	WithFeatureNonTrappingFloatToIntConversion(bool) RuntimeConfig

	// WithFeatureReferenceTypes enables various instructions and features related to table and new reference types.
	//
	// * Introduction of new value types: `funcref` and `externref`.
	// * Support for the following new instructions:
	//   * `ref.null`
	//   * `ref.func`
	//   * `ref.is_null`
	//   * `table.fill`
	//   * `table.get`
	//   * `table.grow`
	//   * `table.set`
	//   * `table.size`
	// * Support for multiple tables per module:
	//   * `call_indirect`, `table.init`, `table.copy` and `elem.drop` instructions can take non-zero table index.
	//   * Element segments can take non-zero table index.
	//
	// Note: "reference-types" is mixed with the "bulk-memory-operations" proposal
	// due to the WebAssembly Working Group merging them "mutually dependent".
	// Therefore, enabling this feature results in enabling WithFeatureBulkMemoryOperations, and vice-versa.
	// See https://github.com/WebAssembly/spec/blob/main/proposals/bulk-memory-operations/Overview.md
	// See https://github.com/WebAssembly/spec/blob/main/proposals/reference-types/Overview.md
	// See https://github.com/WebAssembly/spec/pull/1287
	WithFeatureReferenceTypes(enabled bool) RuntimeConfig

	// WithFeatureSignExtensionOps enables sign extension instructions ("sign-extension-ops"). This defaults to false
	// as the feature was not in WebAssembly 1.0.
	//
	// Here are the notable effects:
	// * Adds instructions `i32.extend8_s`, `i32.extend16_s`, `i64.extend8_s`, `i64.extend16_s` and `i64.extend32_s`
	//
	// See https://github.com/WebAssembly/spec/blob/main/proposals/sign-extension-ops/Overview.md
	WithFeatureSignExtensionOps(bool) RuntimeConfig

	// WithMemoryCapacityPages is a function that determines memory capacity in pages (65536 bytes per page). The input
	// are the min and possibly nil max defined by the module, and the default is to return the min.
	//
	// Ex. To set capacity to max when exists:
	//	c = c.WithMemoryCapacityPages(func(minPages uint32, maxPages *uint32) uint32 {
	//		if maxPages != nil {
	//			return *maxPages
	//		}
	//		return minPages
	//	})
	//
	// This function is used at compile time (ModuleBuilder.Build or Runtime.CompileModule). Compile will err if the
	// function returns a value lower than minPages or greater than WithMemoryLimitPages.
	WithMemoryCapacityPages(func(minPages uint32, maxPages *uint32) uint32) RuntimeConfig

	// WithMemoryLimitPages limits the maximum number of pages a module can define from 65536 pages (4GiB) to the input.
	//
	// Notes:
	// * If a module defines no memory max value, Runtime.CompileModule sets max to the limit.
	// * If a module defines a memory max larger than this limit, it will fail to compile (Runtime.CompileModule).
	// * Any "memory.grow" instruction that results in a larger value than this results in an error at runtime.
	// * Zero is a valid value and results in a crash if any module uses memory.
	//
	// See https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/#grow-mem
	// See https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/#memory-types%E2%91%A0
	WithMemoryLimitPages(uint32) RuntimeConfig

	// WithWasmCore1 enables features included in the WebAssembly Core Specification 1.0. Selecting this
	// overwrites any currently accumulated features with only those included in this W3C recommendation.
	//
	// This is default because as of mid 2022, this is the only version that is a Web Standard (W3C Recommendation).
	//
	// You can select the latest draft of the WebAssembly Core Specification 2.0 instead via WithWasmCore2. You can
	// also enable or disable individual features via `WithXXX` methods. Ex.
	//	rConfig = wazero.NewRuntimeConfig().WithWasmCore1().WithFeatureMutableGlobal(false)
	//
	// See https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/
	WithWasmCore1() RuntimeConfig

	// WithWasmCore2 enables features included in the WebAssembly Core Specification 2.0 (20220419). Selecting this
	// overwrites any currently accumulated features with only those included in this W3C working draft.
	//
	// This is not default because it is not yet incomplete and also not yet a Web Standard (W3C Recommendation).
	//
	// Even after selecting this, you can enable or disable individual features via `WithXXX` methods. Ex.
	//	rConfig = wazero.NewRuntimeConfig().WithWasmCore2().WithFeatureMutableGlobal(false)
	//
	// See https://www.w3.org/TR/2022/WD-wasm-core-2-20220419/
	WithWasmCore2() RuntimeConfig
}

type runtimeConfig struct {
	enabledFeatures     wasm.Features
	newEngine           func(wasm.Features) wasm.Engine
	memoryLimitPages    uint32
	memoryCapacityPages func(minPages uint32, maxPages *uint32) uint32
}

// engineLessConfig helps avoid copy/pasting the wrong defaults.
var engineLessConfig = &runtimeConfig{
	enabledFeatures:     wasm.Features20191205,
	memoryLimitPages:    wasm.MemoryLimitPages,
	memoryCapacityPages: func(minPages uint32, maxPages *uint32) uint32 { return minPages },
}

// NewRuntimeConfigJIT compiles WebAssembly modules into runtime.GOARCH-specific assembly for optimal performance.
//
// Note: This panics at runtime the runtime.GOOS or runtime.GOARCH does not support JIT. Use NewRuntimeConfig to safely
// detect and fallback to NewRuntimeConfigInterpreter if needed.
func NewRuntimeConfigJIT() RuntimeConfig {
	ret := *engineLessConfig // copy
	ret.newEngine = jit.NewEngine
	return &ret
}

// NewRuntimeConfigInterpreter interprets WebAssembly modules instead of compiling them into assembly.
func NewRuntimeConfigInterpreter() RuntimeConfig {
	ret := *engineLessConfig // copy
	ret.newEngine = interpreter.NewEngine
	return &ret
}

// WithFeatureBulkMemoryOperations implements RuntimeConfig.WithFeatureBulkMemoryOperations
func (c *runtimeConfig) WithFeatureBulkMemoryOperations(enabled bool) RuntimeConfig {
	ret := *c // copy
	ret.enabledFeatures = ret.enabledFeatures.Set(wasm.FeatureBulkMemoryOperations, enabled)
	// bulk-memory-operations proposal is mutually-dependant with reference-types proposal.
	ret.enabledFeatures = ret.enabledFeatures.Set(wasm.FeatureReferenceTypes, enabled)
	return &ret
}

// WithFeatureMultiValue implements RuntimeConfig.WithFeatureMultiValue
func (c *runtimeConfig) WithFeatureMultiValue(enabled bool) RuntimeConfig {
	ret := *c // copy
	ret.enabledFeatures = ret.enabledFeatures.Set(wasm.FeatureMultiValue, enabled)
	return &ret
}

// WithFeatureMutableGlobal implements RuntimeConfig.WithFeatureMutableGlobal
func (c *runtimeConfig) WithFeatureMutableGlobal(enabled bool) RuntimeConfig {
	ret := *c // copy
	ret.enabledFeatures = ret.enabledFeatures.Set(wasm.FeatureMutableGlobal, enabled)
	return &ret
}

// WithFeatureNonTrappingFloatToIntConversion implements RuntimeConfig.WithFeatureNonTrappingFloatToIntConversion
func (c *runtimeConfig) WithFeatureNonTrappingFloatToIntConversion(enabled bool) RuntimeConfig {
	ret := *c // copy
	ret.enabledFeatures = ret.enabledFeatures.Set(wasm.FeatureNonTrappingFloatToIntConversion, enabled)
	return &ret
}

// WithFeatureReferenceTypes implements RuntimeConfig.WithFeatureReferenceTypes
func (c *runtimeConfig) WithFeatureReferenceTypes(enabled bool) RuntimeConfig {
	ret := *c // copy
	ret.enabledFeatures = ret.enabledFeatures.Set(wasm.FeatureReferenceTypes, enabled)
	// reference-types proposal is mutually-dependant with bulk-memory-operations proposal.
	ret.enabledFeatures = ret.enabledFeatures.Set(wasm.FeatureBulkMemoryOperations, enabled)
	return &ret
}

// WithFeatureSignExtensionOps implements RuntimeConfig.WithFeatureSignExtensionOps
func (c *runtimeConfig) WithFeatureSignExtensionOps(enabled bool) RuntimeConfig {
	ret := *c // copy
	ret.enabledFeatures = ret.enabledFeatures.Set(wasm.FeatureSignExtensionOps, enabled)
	return &ret
}

// WithMemoryCapacityPages implements RuntimeConfig.WithMemoryCapacityPages
func (c *runtimeConfig) WithMemoryCapacityPages(maxCapacityPages func(minPages uint32, maxPages *uint32) uint32) RuntimeConfig {
	if maxCapacityPages == nil {
		return c // Instead of erring.
	}
	ret := *c // copy
	ret.memoryCapacityPages = maxCapacityPages
	return &ret
}

// WithMemoryLimitPages implements RuntimeConfig.WithMemoryLimitPages
func (c *runtimeConfig) WithMemoryLimitPages(memoryLimitPages uint32) RuntimeConfig {
	ret := *c // copy
	ret.memoryLimitPages = memoryLimitPages
	return &ret
}

// WithWasmCore1 implements RuntimeConfig.WithWasmCore1
func (c *runtimeConfig) WithWasmCore1() RuntimeConfig {
	ret := *c // copy
	ret.enabledFeatures = wasm.Features20191205
	return &ret
}

// WithWasmCore2 implements RuntimeConfig.WithWasmCore2
func (c *runtimeConfig) WithWasmCore2() RuntimeConfig {
	ret := *c // copy
	ret.enabledFeatures = wasm.Features20220419
	return &ret
}

// CompiledCode is a WebAssembly 1.0 module ready to be instantiated (Runtime.InstantiateModule) as an
// api.Module.
//
// Note: In WebAssembly language, this is a decoded, validated, and possibly also compiled module. wazero avoids using
// the name "Module" for both before and after instantiation as the name conflation has caused confusion.
// See https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/#semantic-phases%E2%91%A0
type CompiledCode interface {
	// Close releases all the allocated resources for this CompiledCode.
	//
	// Note: It is safe to call Close while having outstanding calls from Modules instantiated from this CompiledCode.
	Close(context.Context) error
}

type compiledCode struct {
	module *wasm.Module
	// compiledEngine holds an engine on which `module` is compiled.
	compiledEngine wasm.Engine
}

// Close implements CompiledCode.Close
func (c *compiledCode) Close(_ context.Context) error {
	// Note: If you use the context.Context param, don't forget to coerce nil to context.Background()!

	c.compiledEngine.DeleteCompiledModule(c.module)
	// It is possible the underlying may need to return an error later, but in any case this matches api.Module.Close.
	return nil
}

// ModuleConfig configures resources needed by functions that have low-level interactions with the host operating
// system. Using this, resources such as STDIN can be isolated, so that the same module can be safely instantiated
// multiple times.
//
// Note: While wazero supports Windows as a platform, host functions using ModuleConfig follow a UNIX dialect.
// See RATIONALE.md for design background and relationship to WebAssembly System Interfaces (WASI).
//
// Note: ModuleConfig is immutable. Each WithXXX function returns a new instance including the corresponding change.
type ModuleConfig interface {

	// WithArgs assigns command-line arguments visible to an imported function that reads an arg vector (argv). Defaults to
	// none.
	//
	// These values are commonly read by the functions like "args_get" in "wasi_snapshot_preview1" although they could be
	// read by functions imported from other modules.
	//
	// Similar to os.Args and exec.Cmd Env, many implementations would expect a program name to be argv[0]. However, neither
	// WebAssembly nor WebAssembly System Interfaces (WASI) define this. Regardless, you may choose to set the first
	// argument to the same value set via WithName.
	//
	// Note: This does not default to os.Args as that violates sandboxing.
	// Note: Runtime.InstantiateModule errs if any value is empty.
	// See https://linux.die.net/man/3/argv
	// See https://en.wikipedia.org/wiki/Null-terminated_string
	WithArgs(...string) ModuleConfig

	// WithEnv sets an environment variable visible to a Module that imports functions. Defaults to none.
	//
	// Validation is the same as os.Setenv on Linux and replaces any existing value. Unlike exec.Cmd Env, this does not
	// default to the current process environment as that would violate sandboxing. This also does not preserve order.
	//
	// Environment variables are commonly read by the functions like "environ_get" in "wasi_snapshot_preview1" although
	// they could be read by functions imported from other modules.
	//
	// While similar to process configuration, there are no assumptions that can be made about anything OS-specific. For
	// example, neither WebAssembly nor WebAssembly System Interfaces (WASI) define concerns processes have, such as
	// case-sensitivity on environment keys. For portability, define entries with case-insensitively unique keys.
	//
	// Note: Runtime.InstantiateModule errs if the key is empty or contains a NULL(0) or equals("") character.
	// See https://linux.die.net/man/3/environ
	// See https://en.wikipedia.org/wiki/Null-terminated_string
	WithEnv(key, value string) ModuleConfig

	// WithFS assigns the file system to use for any paths beginning at "/". Defaults to not found.
	//
	// Ex. This sets a read-only, embedded file-system to serve files under the root ("/") and working (".") directories:
	//
	//	//go:embed testdata/index.html
	//	var testdataIndex embed.FS
	//
	//	rooted, err := fs.Sub(testdataIndex, "testdata")
	//	require.NoError(t, err)
	//
	//	// "index.html" is accessible as both "/index.html" and "./index.html" because we didn't use WithWorkDirFS.
	//	config := wazero.NewModuleConfig().WithFS(rooted)
	//
	// Note: This sets WithWorkDirFS to the same file-system unless already set.
	WithFS(fs.FS) ModuleConfig

	// WithImport replaces a specific import module and name with a new one. This allows you to break up a monolithic
	// module imports, such as "env". This can also help reduce cyclic dependencies.
	//
	// For example, if a module was compiled with one module owning all imports:
	//	(import "js" "tbl" (table $tbl 4 funcref))
	//	(import "js" "increment" (func $increment (result i32)))
	//	(import "js" "decrement" (func $decrement (result i32)))
	//	(import "js" "wasm_increment" (func $wasm_increment (result i32)))
	//	(import "js" "wasm_decrement" (func $wasm_decrement (result i32)))
	//
	// Use this function to import "increment" and "decrement" from the module "go" and other imports from "wasm":
	//	config.WithImportModule("js", "wasm")
	//	config.WithImport("wasm", "increment", "go", "increment")
	//	config.WithImport("wasm", "decrement", "go", "decrement")
	//
	// Upon instantiation, imports resolve as if they were compiled like so:
	//	(import "wasm" "tbl" (table $tbl 4 funcref))
	//	(import "go" "increment" (func $increment (result i32)))
	//	(import "go" "decrement" (func $decrement (result i32)))
	//	(import "wasm" "wasm_increment" (func $wasm_increment (result i32)))
	//	(import "wasm" "wasm_decrement" (func $wasm_decrement (result i32)))
	//
	// Note: Any WithImport instructions happen in order, after any WithImportModule instructions.
	WithImport(oldModule, oldName, newModule, newName string) ModuleConfig

	// WithImportModule replaces every import with oldModule with newModule. This is helpful for modules who have
	// transitioned to a stable status since the underlying wasm was compiled.
	//
	// For example, if a module was compiled like below, with an old module for WASI:
	//	(import "wasi_unstable" "args_get" (func (param i32, i32) (result i32)))
	//
	// Use this function to update it to the current version:
	//	config.WithImportModule("wasi_unstable", wasi.ModuleSnapshotPreview1)
	//
	// See WithImport for a comprehensive example.
	// Note: Any WithImportModule instructions happen in order, before any WithImport instructions.
	WithImportModule(oldModule, newModule string) ModuleConfig

	// WithName configures the module name. Defaults to what was decoded from the module source.
	//
	// If the source was in WebAssembly 1.0 Binary Format, this defaults to what was decoded from the custom name
	// section. Otherwise, if it was decoded from Text Format, this defaults to the module ID stripped of leading '$'.
	//
	// For example, if the Module was decoded from the text format `(module $math)`, the default name is "math".
	//
	// See https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/#name-section%E2%91%A0
	// See https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/#custom-section%E2%91%A0
	// See https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/#modules%E2%91%A0%E2%91%A2
	WithName(string) ModuleConfig

	// WithStartFunctions configures the functions to call after the module is instantiated. Defaults to "_start".
	//
	// Note: If any function doesn't exist, it is skipped. However, all functions that do exist are called in order.
	WithStartFunctions(...string) ModuleConfig

	// WithStderr configures where standard error (file descriptor 2) is written. Defaults to io.Discard.
	//
	// This writer is most commonly used by the functions like "fd_write" in "wasi_snapshot_preview1" although it could
	// be used by functions imported from other modules.
	//
	// Note: The caller is responsible to close any io.Writer they supply: It is not closed on api.Module Close.
	// Note: This does not default to os.Stderr as that both violates sandboxing and prevents concurrent modules.
	// See https://linux.die.net/man/3/stderr
	WithStderr(io.Writer) ModuleConfig

	// WithStdin configures where standard input (file descriptor 0) is read. Defaults to return io.EOF.
	//
	// This reader is most commonly used by the functions like "fd_read" in "wasi_snapshot_preview1" although it could
	// be used by functions imported from other modules.
	//
	// Note: The caller is responsible to close any io.Reader they supply: It is not closed on api.Module Close.
	// Note: This does not default to os.Stdin as that both violates sandboxing and prevents concurrent modules.
	// See https://linux.die.net/man/3/stdin
	WithStdin(io.Reader) ModuleConfig

	// WithStdout configures where standard output (file descriptor 1) is written. Defaults to io.Discard.
	//
	// This writer is most commonly used by the functions like "fd_write" in "wasi_snapshot_preview1" although it could
	// be used by functions imported from other modules.
	//
	// Note: The caller is responsible to close any io.Writer they supply: It is not closed on api.Module Close.
	// Note: This does not default to os.Stdout as that both violates sandboxing and prevents concurrent modules.
	// See https://linux.die.net/man/3/stdout
	WithStdout(io.Writer) ModuleConfig

	// WithWorkDirFS indicates the file system to use for any paths beginning at "./". Defaults to the same as WithFS.
	//
	// Ex. This sets a read-only, embedded file-system as the root ("/"), and a mutable one as the working directory ("."):
	//
	//	//go:embed appA
	//	var rootFS embed.FS
	//
	//	// Files relative to this source under appA are available under "/" and files relative to "/work/appA" under ".".
	//	config := wazero.NewModuleConfig().WithFS(rootFS).WithWorkDirFS(os.DirFS("/work/appA"))
	//
	// Note: os.DirFS documentation includes important notes about isolation, which also applies to fs.Sub. As of Go 1.18,
	// the built-in file-systems are not jailed (chroot). See https://github.com/golang/go/issues/42322
	WithWorkDirFS(fs.FS) ModuleConfig
}

type moduleConfig struct {
	name           string
	startFunctions []string
	stdin          io.Reader
	stdout         io.Writer
	stderr         io.Writer
	args           []string
	// environ is pair-indexed to retain order similar to os.Environ.
	environ []string
	// environKeys allow overwriting of existing values.
	environKeys map[string]int

	// preopenFD has the next FD number to use
	preopenFD uint32
	// preopens are keyed on file descriptor and only include the Path and FS fields.
	preopens map[uint32]*wasm.FileEntry
	// preopenPaths allow overwriting of existing paths.
	preopenPaths map[string]uint32
	// replacedImports holds the latest state of WithImport
	// Note: Key is NUL delimited as import module and name can both include any UTF-8 characters.
	replacedImports map[string][2]string
	// replacedImportModules holds the latest state of WithImportModule
	replacedImportModules map[string]string
}

func NewModuleConfig() ModuleConfig {
	return &moduleConfig{
		startFunctions: []string{"_start"},
		environKeys:    map[string]int{},
		preopenFD:      uint32(3), // after stdin/stdout/stderr
		preopens:       map[uint32]*wasm.FileEntry{},
		preopenPaths:   map[string]uint32{},
	}
}

// WithArgs implements ModuleConfig.WithArgs
func (c *moduleConfig) WithArgs(args ...string) ModuleConfig {
	ret := *c // copy
	ret.args = args
	return &ret
}

// WithEnv implements ModuleConfig.WithEnv
func (c *moduleConfig) WithEnv(key, value string) ModuleConfig {
	ret := *c // copy
	// Check to see if this key already exists and update it.
	if i, ok := ret.environKeys[key]; ok {
		ret.environ[i+1] = value // environ is pair-indexed, so the value is 1 after the key.
	} else {
		ret.environKeys[key] = len(ret.environ)
		ret.environ = append(ret.environ, key, value)
	}
	return &ret
}

// WithFS implements ModuleConfig.WithFS
func (c *moduleConfig) WithFS(fs fs.FS) ModuleConfig {
	ret := *c // copy
	ret.setFS("/", fs)
	return &ret
}

// WithImport implements ModuleConfig.WithImport
func (c *moduleConfig) WithImport(oldModule, oldName, newModule, newName string) ModuleConfig {
	ret := *c // copy
	if ret.replacedImports == nil {
		ret.replacedImports = map[string][2]string{}
	}
	var builder strings.Builder
	builder.WriteString(oldModule)
	builder.WriteByte(0) // delimit with NUL as module and name can be any UTF-8 characters.
	builder.WriteString(oldName)
	ret.replacedImports[builder.String()] = [2]string{newModule, newName}
	return &ret
}

// WithImportModule implements ModuleConfig.WithImportModule
func (c *moduleConfig) WithImportModule(oldModule, newModule string) ModuleConfig {
	ret := *c // copy
	if ret.replacedImportModules == nil {
		ret.replacedImportModules = map[string]string{}
	}
	ret.replacedImportModules[oldModule] = newModule
	return &ret
}

// WithName implements ModuleConfig.WithName
func (c *moduleConfig) WithName(name string) ModuleConfig {
	ret := *c // copy
	ret.name = name
	return &ret
}

// WithStartFunctions implements ModuleConfig.WithStartFunctions
func (c *moduleConfig) WithStartFunctions(startFunctions ...string) ModuleConfig {
	ret := *c // copy
	ret.startFunctions = startFunctions
	return &ret
}

// WithStderr implements ModuleConfig.WithStderr
func (c *moduleConfig) WithStderr(stderr io.Writer) ModuleConfig {
	ret := *c // copy
	ret.stderr = stderr
	return &ret
}

// WithStdin implements ModuleConfig.WithStdin
func (c *moduleConfig) WithStdin(stdin io.Reader) ModuleConfig {
	ret := *c // copy
	ret.stdin = stdin
	return &ret
}

// WithStdout implements ModuleConfig.WithStdout
func (c *moduleConfig) WithStdout(stdout io.Writer) ModuleConfig {
	ret := *c // copy
	ret.stdout = stdout
	return &ret
}

// WithWorkDirFS implements ModuleConfig.WithWorkDirFS
func (c *moduleConfig) WithWorkDirFS(fs fs.FS) ModuleConfig {
	ret := *c // copy
	ret.setFS(".", fs)
	return &ret
}

// setFS maps a path to a file-system. This is only used for base paths: "/" and ".".
func (c *moduleConfig) setFS(path string, fs fs.FS) {
	// Check to see if this key already exists and update it.
	entry := &wasm.FileEntry{Path: path, FS: fs}
	if fd, ok := c.preopenPaths[path]; ok {
		c.preopens[fd] = entry
	} else {
		c.preopens[c.preopenFD] = entry
		c.preopenPaths[path] = c.preopenFD
		c.preopenFD++
	}
}

// toSysContext creates a baseline wasm.SysContext configured by ModuleConfig.
func (c *moduleConfig) toSysContext() (sys *wasm.SysContext, err error) {
	var environ []string // Intentionally doesn't pre-allocate to reduce logic to default to nil.
	// Same validation as syscall.Setenv for Linux
	for i := 0; i < len(c.environ); i += 2 {
		key, value := c.environ[i], c.environ[i+1]
		if len(key) == 0 {
			err = errors.New("environ invalid: empty key")
			return
		}
		for j := 0; j < len(key); j++ {
			if key[j] == '=' { // NUL enforced in NewSysContext
				err = errors.New("environ invalid: key contains '=' character")
				return
			}
		}
		environ = append(environ, key+"="+value)
	}

	// Ensure no-one set a nil FD. We do this here instead of at the call site to allow chaining as nil is unexpected.
	rootFD := uint32(0) // zero is invalid
	setWorkDirFS := false
	preopens := c.preopens
	for fd, entry := range preopens {
		if entry.FS == nil {
			err = fmt.Errorf("FS for %s is nil", entry.Path)
			return
		} else if entry.Path == "/" {
			rootFD = fd
		} else if entry.Path == "." {
			setWorkDirFS = true
		}
	}

	// Default the working directory to the root FS if it exists.
	if rootFD != 0 && !setWorkDirFS {
		preopens[c.preopenFD] = &wasm.FileEntry{Path: ".", FS: preopens[rootFD].FS}
	}

	return wasm.NewSysContext(math.MaxUint32, c.args, environ, c.stdin, c.stdout, c.stderr, preopens)
}

func (c *moduleConfig) replaceImports(module *wasm.Module) *wasm.Module {
	if (c.replacedImportModules == nil && c.replacedImports == nil) || module.ImportSection == nil {
		return module
	}

	changed := false

	ret := *module // shallow copy
	replacedImports := make([]*wasm.Import, len(module.ImportSection))
	copy(replacedImports, module.ImportSection)

	// First, replace any import.Module
	for oldModule, newModule := range c.replacedImportModules {
		for i, imp := range replacedImports {
			if imp.Module == oldModule {
				changed = true
				cp := *imp // shallow copy
				cp.Module = newModule
				replacedImports[i] = &cp
			} else {
				replacedImports[i] = imp
			}
		}
	}

	// Now, replace any import.Module+import.Name
	for oldImport, newImport := range c.replacedImports {
		for i, imp := range replacedImports {
			nulIdx := strings.IndexByte(oldImport, 0)
			oldModule := oldImport[0:nulIdx]
			oldName := oldImport[nulIdx+1:]
			if imp.Module == oldModule && imp.Name == oldName {
				changed = true
				cp := *imp // shallow copy
				cp.Module = newImport[0]
				cp.Name = newImport[1]
				replacedImports[i] = &cp
			} else {
				replacedImports[i] = imp
			}
		}
	}

	if !changed {
		return module
	}
	ret.ImportSection = replacedImports
	return &ret
}

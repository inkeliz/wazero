package wasm

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/tetratelabs/wazero/api"
	experimentalapi "github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/internal/ieee754"
	"github.com/tetratelabs/wazero/internal/leb128"
)

type (
	// Store is the runtime representation of "instantiated" Wasm module and objects.
	// Multiple modules can be instantiated within a single store, and each instance,
	// (e.g. function instance) can be referenced by other module instances in a Store via Module.ImportSection.
	//
	// Every type whose name ends with "Instance" suffix belongs to exactly one store.
	//
	// Note that store is not thread (concurrency) safe, meaning that using single Store
	// via multiple goroutines might result in race conditions. In that case, the invocation
	// and access to any methods and field of Store must be guarded by mutex.
	//
	// See https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/#store%E2%91%A0
	Store struct {
		// EnabledFeatures are read-only to allow optimizations.
		EnabledFeatures Features

		// Engine is a global context for a Store which is in responsible for compilation and execution of Wasm modules.
		Engine Engine

		// moduleNames ensures no race conditions instantiating two modules of the same name
		moduleNames map[string]struct{} // guarded by mux

		// modules holds the instantiated Wasm modules by module name from Instantiate.
		modules map[string]*ModuleInstance // guarded by mux

		// typeIDs maps each FunctionType.String() to a unique FunctionTypeID. This is used at runtime to
		// do type-checks on indirect function calls.
		typeIDs map[string]FunctionTypeID

		// functionMaxTypes represents the limit on the number of function types in a store.
		// Note: this is fixed to 2^27 but have this a field for testability.
		functionMaxTypes uint32

		// mux is used to guard the fields from concurrent access.
		mux sync.RWMutex
	}

	// ModuleInstance represents instantiated wasm module.
	// The difference from the spec is that in wazero, a ModuleInstance holds pointers
	// to the instances, rather than "addresses" (i.e. index to Store.Functions, Globals, etc) for convenience.
	//
	// See https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/#syntax-moduleinst
	ModuleInstance struct {
		Name      string
		Exports   map[string]*ExportInstance
		Functions []*FunctionInstance
		Globals   []*GlobalInstance
		// Memory is set when Module.MemorySection had a memory, regardless of whether it was exported.
		Memory *MemoryInstance
		Tables []*TableInstance
		Types  []*FunctionType

		// CallCtx holds default function call context from this function instance.
		CallCtx *CallContext

		// Engine implements function calls for this module.
		Engine ModuleEngine

		// TypeIDs is index-correlated with types and holds typeIDs which is uniquely assigned to a type by store.
		// This is necessary to achieve fast runtime type checking for indirect function calls at runtime.
		TypeIDs []FunctionTypeID

		// DataInstances holds data segments bytes of the module.
		// This is only used by bulk memory operations.
		//
		// https://www.w3.org/TR/2022/WD-wasm-core-2-20220419/exec/runtime.html#data-instances
		DataInstances []DataInstance

		// ElementInstances holds the element instance, and each holds the references to either functions
		// or external objects (unimplemented).
		ElementInstances []ElementInstance
	}

	// DataInstance holds bytes corresponding to the data segment in a module.
	//
	// https://www.w3.org/TR/2022/WD-wasm-core-2-20220419/exec/runtime.html#data-instances
	DataInstance = []byte

	// ExportInstance represents an exported instance in a Store.
	// The difference from the spec is that in wazero, a ExportInstance holds pointers
	// to the instances, rather than "addresses" (i.e. index to Store.Functions, Globals, etc) for convenience.
	//
	// See https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/#syntax-exportinst
	ExportInstance struct {
		Type     ExternType
		Function *FunctionInstance
		Global   *GlobalInstance
		Memory   *MemoryInstance
		Table    *TableInstance
	}

	// FunctionInstance represents a function instance in a Store.
	// See https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/#function-instances%E2%91%A0
	FunctionInstance struct {
		// DebugName is for debugging purpose, and is used to augment stack traces.
		DebugName string

		// Kind describes how this function should be called.
		Kind FunctionKind

		// Type is the signature of this function.
		Type *FunctionType

		// LocalTypes holds types of locals, set when Kind == FunctionKindWasm
		LocalTypes []ValueType

		// Body is the function body in WebAssembly Binary Format, set when Kind == FunctionKindWasm
		Body []byte

		// GoFunc holds the runtime representation of host functions.
		// This is nil when Kind == FunctionKindWasm. Otherwise, all the above fields are ignored as they are
		// specific to Wasm functions.
		GoFunc *reflect.Value

		// Fields above here are settable prior to instantiation. Below are set by the Store during instantiation.

		// ModuleInstance holds the pointer to the module instance to which this function belongs.
		Module *ModuleInstance

		// TypeID is assigned by a store for FunctionType.
		TypeID FunctionTypeID

		// Idx holds the index of this function instance in the function index namespace (beginning with imports).
		Idx Index

		// The below metadata are used in function listeners, prior to instantiation:

		// moduleName is the defining module's name
		moduleName string

		// name is the module-defined name of this function
		name string

		// paramNames is non-nil when all parameters have names.
		paramNames []string

		// exportNames is non-nil when the function is exported.
		exportNames []string

		// FunctionListener holds a listener to notify when this function is called.
		FunctionListener experimentalapi.FunctionListener
	}

	// GlobalInstance represents a global instance in a store.
	// See https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/#global-instances%E2%91%A0
	GlobalInstance struct {
		Type *GlobalType
		// Val holds a 64-bit representation of the actual value.
		Val uint64
		// ^^ TODO: this should be guarded with atomics when mutable
	}

	// FunctionTypeID is a uniquely assigned integer for a function type.
	// This is wazero specific runtime object and specific to a store,
	// and used at runtime to do type-checks on indirect function calls.
	FunctionTypeID uint32
)

// Index implements the same method as documented on experimental.FunctionDefinition.
func (f *FunctionInstance) Index() uint32 {
	return f.Idx
}

// Name implements the same method as documented on experimental.FunctionDefinition.
func (f *FunctionInstance) Name() string {
	return f.name
}

// ModuleName implements the same method as documented on experimental.FunctionDefinition.
func (f *FunctionInstance) ModuleName() string {
	return f.moduleName
}

// ExportNames implements the same method as documented on experimental.FunctionDefinition.
func (f *FunctionInstance) ExportNames() []string {
	return f.exportNames
}

// ParamNames implements the same method as documented on experimental.FunctionDefinition.
func (f *FunctionInstance) ParamNames() []string {
	return f.paramNames
}

// The wazero specific limitations described at RATIONALE.md.
const (
	maximumFunctionTypes = 1 << 27
)

// addSections adds section elements to the ModuleInstance
func (m *ModuleInstance) addSections(module *Module, importedFunctions, functions []*FunctionInstance,
	importedGlobals, globals []*GlobalInstance, tables []*TableInstance, memory, importedMemory *MemoryInstance,
	types []*FunctionType, typeIDs []FunctionTypeID) {

	m.Types = types
	m.TypeIDs = typeIDs

	m.Functions = append(m.Functions, importedFunctions...)
	for i, f := range functions {
		// Associate each function with the type instance and the module instance's pointer.
		f.Module = m
		f.TypeID = typeIDs[module.FunctionSection[i]]
		m.Functions = append(m.Functions, f)
	}

	m.Globals = append(m.Globals, importedGlobals...)
	m.Globals = append(m.Globals, globals...)

	m.Tables = tables

	if importedMemory != nil {
		m.Memory = importedMemory
	} else {
		m.Memory = memory
	}

	m.buildExports(module.ExportSection)
	m.buildDataInstances(module.DataSection)
}

func (m *ModuleInstance) buildDataInstances(segments []*DataSegment) {
	for _, d := range segments {
		m.DataInstances = append(m.DataInstances, d.Init)
	}
}

func (m *ModuleInstance) buildElementInstances(elements []*ElementSegment) {
	m.ElementInstances = make([]ElementInstance, len(elements))
	for i, elm := range elements {
		if elm.Type == RefTypeFuncref && elm.Mode == ElementModePassive {
			// Only passive elements can be access as element instances.
			// See https://www.w3.org/TR/2022/WD-wasm-core-2-20220419/syntax/modules.html#element-segments
			m.ElementInstances[i] = *m.Engine.CreateFuncElementInstance(elm.Init)
		}
	}
}

func (m *ModuleInstance) buildExports(exports []*Export) {
	m.Exports = make(map[string]*ExportInstance, len(exports))
	for _, exp := range exports {
		index := exp.Index
		var ei *ExportInstance
		switch exp.Type {
		case ExternTypeFunc:
			ei = &ExportInstance{Type: exp.Type, Function: m.Functions[index]}
		case ExternTypeGlobal:
			ei = &ExportInstance{Type: exp.Type, Global: m.Globals[index]}
		case ExternTypeMemory:
			ei = &ExportInstance{Type: exp.Type, Memory: m.Memory}
		case ExternTypeTable:
			ei = &ExportInstance{Type: exp.Type, Table: m.Tables[index]}
		}

		// We already validated the duplicates during module validation phase.
		m.Exports[exp.Name] = ei
	}
}

func (m *ModuleInstance) validateData(data []*DataSegment) (err error) {
	for _, d := range data {
		if !d.IsPassive() {
			offset := int(executeConstExpression(m.Globals, d.OffsetExpression).(int32))
			ceil := offset + len(d.Init)
			if offset < 0 || ceil > len(m.Memory.Buffer) {
				return fmt.Errorf("out of bounds memory access")
			}
		}

	}
	return
}

func (m *ModuleInstance) applyData(data []*DataSegment) {
	for _, d := range data {
		if !d.IsPassive() {
			offset := executeConstExpression(m.Globals, d.OffsetExpression).(int32)
			copy(m.Memory.Buffer[offset:], d.Init)
		}
	}
}

// GetExport returns an export of the given name and type or errs if not exported or the wrong type.
func (m *ModuleInstance) getExport(name string, et ExternType) (*ExportInstance, error) {
	exp, ok := m.Exports[name]
	if !ok {
		return nil, fmt.Errorf("%q is not exported in module %q", name, m.Name)
	}
	if exp.Type != et {
		return nil, fmt.Errorf("export %q in module %q is a %s, not a %s", name, m.Name, ExternTypeName(exp.Type), ExternTypeName(et))
	}
	return exp, nil
}

func NewStore(enabledFeatures Features, engine Engine) *Store {
	return &Store{
		EnabledFeatures:  enabledFeatures,
		Engine:           engine,
		moduleNames:      map[string]struct{}{},
		modules:          map[string]*ModuleInstance{},
		typeIDs:          map[string]FunctionTypeID{},
		functionMaxTypes: maximumFunctionTypes,
	}
}

// Instantiate uses name instead of the Module.NameSection ModuleName as it allows instantiating the same module under
// different names safely and concurrently.
//
// * ctx: the default context used for function calls.
// * name: the name of the module.
// * sys: the system context, which will be closed (SysContext.Close) on CallContext.Close.
//
// Note: Module.Validate must be called prior to instantiation.
func (s *Store) Instantiate(
	ctx context.Context,
	module *Module,
	name string,
	sys *SysContext,
	functionListenerFactory experimentalapi.FunctionListenerFactory,
) (*CallContext, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if err := s.requireModuleName(name); err != nil {
		return nil, err
	}

	typeIDs, err := s.getFunctionTypeIDs(module.TypeSection)
	if err != nil {
		s.deleteModule(name)
		return nil, err
	}

	importedFunctions, importedGlobals, importedTables, importedMemory, err := s.resolveImports(module)
	if err != nil {
		s.deleteModule(name)
		return nil, err
	}

	tables, tableInit, err := module.buildTables(importedTables, importedGlobals)
	if err != nil {
		s.deleteModule(name)
		return nil, err
	}
	globals, memory := module.buildGlobals(importedGlobals), module.buildMemory()

	// If there are no module-defined functions, assume this is a host module.
	var functions []*FunctionInstance
	var funcSection SectionID
	if module.HostFunctionSection == nil {
		funcSection = SectionIDFunction
		functions = module.buildFunctions(name, functionListenerFactory)
	} else {
		funcSection = SectionIDHostFunction
		functions = module.buildHostFunctions(name, functionListenerFactory)
	}

	// Now we have all instances from imports and local ones, so ready to create a new ModuleInstance.
	m := &ModuleInstance{Name: name}
	m.addSections(module, importedFunctions, functions, importedGlobals, globals, tables, importedMemory, memory, module.TypeSection, typeIDs)

	if err = m.validateData(module.DataSection); err != nil {
		s.deleteModule(name)
		return nil, err
	}

	// Plus, we are ready to compile functions.
	m.Engine, err = s.Engine.NewModuleEngine(name, module, importedFunctions, functions, tables, tableInit)
	if err != nil {
		return nil, fmt.Errorf("compilation failed: %w", err)
	}

	// After engine creation, we can create the funcref element instances.
	m.buildElementInstances(module.ElementSection)

	// Now all the validation passes, we are safe to mutate memory instances (possibly imported ones).
	m.applyData(module.DataSection)

	// Build the default context for calls to this module.
	m.CallCtx = NewCallContext(s, m, sys)

	// Execute the start function.
	if module.StartSection != nil {
		funcIdx := *module.StartSection
		f := m.Functions[funcIdx]
		if _, err = f.Module.Engine.Call(ctx, m.CallCtx, f); err != nil {
			s.deleteModule(name)
			return nil, fmt.Errorf("start %s failed: %w", module.funcDesc(funcSection, funcIdx), err)
		}
	}

	// Now that the instantiation is complete without error, add it. This makes it visible for import.
	s.addModule(m)
	return m.CallCtx, nil
}

// deleteModule makes the moduleName available for instantiation again.
func (s *Store) deleteModule(moduleName string) {
	s.mux.Lock()
	defer s.mux.Unlock()
	delete(s.modules, moduleName)
	delete(s.moduleNames, moduleName)
}

// requireModuleName is a pre-flight check to reserve a module.
// This must be reverted on error with deleteModule if initialization fails.
func (s *Store) requireModuleName(moduleName string) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	if _, ok := s.moduleNames[moduleName]; ok {
		return fmt.Errorf("module %s has already been instantiated", moduleName)
	}
	s.moduleNames[moduleName] = struct{}{}
	return nil
}

// addModule makes the module visible for import
func (s *Store) addModule(m *ModuleInstance) {
	s.mux.Lock()
	defer s.mux.Unlock()
	s.modules[m.Name] = m
}

// Module implements wazero.Runtime Module
func (s *Store) Module(moduleName string) api.Module {
	if m := s.module(moduleName); m != nil {
		return m.CallCtx
	} else {
		return nil
	}
}

func (s *Store) module(moduleName string) *ModuleInstance {
	s.mux.RLock()
	defer s.mux.RUnlock()
	return s.modules[moduleName]
}

func (s *Store) resolveImports(module *Module) (
	importedFunctions []*FunctionInstance, importedGlobals []*GlobalInstance,
	importedTables []*TableInstance, importedMemory *MemoryInstance,
	err error,
) {
	s.mux.RLock()
	defer s.mux.RUnlock()

	for idx, i := range module.ImportSection {
		m, ok := s.modules[i.Module]
		if !ok {
			err = fmt.Errorf("module[%s] not instantiated", i.Module)
			return
		}

		var imported *ExportInstance
		imported, err = m.getExport(i.Name, i.Type)
		if err != nil {
			return
		}

		switch i.Type {
		case ExternTypeFunc:
			typeIndex := i.DescFunc
			// TODO: this shouldn't be possible as invalid should fail validate
			if int(typeIndex) >= len(module.TypeSection) {
				err = errorInvalidImport(i, idx, fmt.Errorf("function type out of range"))
				return
			}
			expectedType := module.TypeSection[i.DescFunc]
			importedFunction := imported.Function

			actualType := importedFunction.Type
			if !expectedType.EqualsSignature(actualType.Params, actualType.Results) {
				err = errorInvalidImport(i, idx, fmt.Errorf("signature mismatch: %s != %s", expectedType, actualType))
				return
			}

			importedFunctions = append(importedFunctions, importedFunction)
		case ExternTypeTable:
			expected := i.DescTable
			importedTable := imported.Table

			if expected.Min > importedTable.Min {
				err = errorMinSizeMismatch(i, idx, expected.Min, importedTable.Min)
				return
			}

			if expected.Max != nil {
				expectedMax := *expected.Max
				if importedTable.Max == nil {
					err = errorNoMax(i, idx, expectedMax)
					return
				} else if expectedMax < *importedTable.Max {
					err = errorMaxSizeMismatch(i, idx, expectedMax, *importedTable.Max)
					return
				}
			}
			importedTables = append(importedTables, importedTable)
		case ExternTypeMemory:
			expected := i.DescMem
			importedMemory = imported.Memory

			if expected.Min > importedMemory.Min {
				err = errorMinSizeMismatch(i, idx, expected.Min, importedMemory.Min)
				return
			}

			if expected.Max < importedMemory.Max {
				err = errorMaxSizeMismatch(i, idx, expected.Max, importedMemory.Max)
				return
			}
		case ExternTypeGlobal:
			expected := i.DescGlobal
			importedGlobal := imported.Global

			if expected.Mutable != importedGlobal.Type.Mutable {
				err = errorInvalidImport(i, idx, fmt.Errorf("mutability mismatch: %t != %t",
					expected.Mutable, importedGlobal.Type.Mutable))
				return
			}

			if expected.ValType != importedGlobal.Type.ValType {
				err = errorInvalidImport(i, idx, fmt.Errorf("value type mismatch: %s != %s",
					ValueTypeName(expected.ValType), ValueTypeName(importedGlobal.Type.ValType)))
				return
			}
			importedGlobals = append(importedGlobals, importedGlobal)
		}
	}
	return
}

func errorMinSizeMismatch(i *Import, idx int, expected, actual uint32) error {
	return errorInvalidImport(i, idx, fmt.Errorf("minimum size mismatch: %d > %d", expected, actual))
}

func errorNoMax(i *Import, idx int, expected uint32) error {
	return errorInvalidImport(i, idx, fmt.Errorf("maximum size mismatch: %d, but actual has no max", expected))
}

func errorMaxSizeMismatch(i *Import, idx int, expected, actual uint32) error {
	return errorInvalidImport(i, idx, fmt.Errorf("maximum size mismatch: %d < %d", expected, actual))
}

func errorInvalidImport(i *Import, idx int, err error) error {
	return fmt.Errorf("import[%d] %s[%s.%s]: %w", idx, ExternTypeName(i.Type), i.Module, i.Name, err)
}

// Global initialization constant expression can only reference the imported globals.
// See the note on https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/#constant-expressions%E2%91%A0
func executeConstExpression(globals []*GlobalInstance, expr *ConstantExpression) (v interface{}) {
	r := bytes.NewReader(expr.Data)
	switch expr.Opcode {
	case OpcodeI32Const:
		// Treat constants as signed as their interpretation is not yet known per /RATIONALE.md
		v, _, _ = leb128.DecodeInt32(r)
	case OpcodeI64Const:
		// Treat constants as signed as their interpretation is not yet known per /RATIONALE.md
		v, _, _ = leb128.DecodeInt64(r)
	case OpcodeF32Const:
		v, _ = ieee754.DecodeFloat32(r)
	case OpcodeF64Const:
		v, _ = ieee754.DecodeFloat64(r)
	case OpcodeGlobalGet:
		id, _, _ := leb128.DecodeUint32(r)
		g := globals[id]
		switch g.Type.ValType {
		case ValueTypeI32:
			v = int32(g.Val)
		case ValueTypeI64:
			v = int64(g.Val)
		case ValueTypeF32:
			v = api.DecodeF32(g.Val)
		case ValueTypeF64:
			v = api.DecodeF64(g.Val)
		}
	}
	return
}

func (s *Store) getFunctionTypeIDs(ts []*FunctionType) ([]FunctionTypeID, error) {
	// We take write-lock here as the following might end up mutating typeIDs map.
	s.mux.Lock()
	defer s.mux.Unlock()
	ret := make([]FunctionTypeID, len(ts))
	for i, t := range ts {
		inst, err := s.getFunctionTypeID(t)
		if err != nil {
			return nil, err
		}
		ret[i] = inst
	}
	return ret, nil
}

func (s *Store) getFunctionTypeID(t *FunctionType) (FunctionTypeID, error) {
	key := t.String()
	id, ok := s.typeIDs[key]
	if !ok {
		l := uint32(len(s.typeIDs))
		if l >= s.functionMaxTypes {
			return 0, fmt.Errorf("too many function types in a store")
		}
		id = FunctionTypeID(len(s.typeIDs))
		s.typeIDs[key] = id
	}
	return id, nil
}

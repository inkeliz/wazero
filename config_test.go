package wazero

import (
	"context"
	"io"
	"math"
	"testing"
	"testing/fstest"

	"github.com/tetratelabs/wazero/internal/testing/require"
	"github.com/tetratelabs/wazero/internal/wasm"
)

func TestRuntimeConfig(t *testing.T) {
	tests := []struct {
		name     string
		with     func(RuntimeConfig) RuntimeConfig
		expected RuntimeConfig
	}{
		{
			name: "WithMemoryLimitPages",
			with: func(c RuntimeConfig) RuntimeConfig {
				return c.WithMemoryLimitPages(1)
			},
			expected: &runtimeConfig{
				memoryLimitPages: 1,
			},
		},
		{
			name: "bulk-memory-operations",
			with: func(c RuntimeConfig) RuntimeConfig {
				return c.WithFeatureBulkMemoryOperations(true)
			},
			expected: &runtimeConfig{
				enabledFeatures: wasm.FeatureBulkMemoryOperations | wasm.FeatureReferenceTypes,
			},
		},
		{
			name: "multi-value",
			with: func(c RuntimeConfig) RuntimeConfig {
				return c.WithFeatureMultiValue(true)
			},
			expected: &runtimeConfig{
				enabledFeatures: wasm.FeatureMultiValue,
			},
		},
		{
			name: "mutable-global",
			with: func(c RuntimeConfig) RuntimeConfig {
				return c.WithFeatureMutableGlobal(true)
			},
			expected: &runtimeConfig{
				enabledFeatures: wasm.FeatureMutableGlobal,
			},
		},
		{
			name: "nontrapping-float-to-int-conversion",
			with: func(c RuntimeConfig) RuntimeConfig {
				return c.WithFeatureNonTrappingFloatToIntConversion(true)
			},
			expected: &runtimeConfig{
				enabledFeatures: wasm.FeatureNonTrappingFloatToIntConversion,
			},
		},
		{
			name: "sign-extension-ops",
			with: func(c RuntimeConfig) RuntimeConfig {
				return c.WithFeatureSignExtensionOps(true)
			},
			expected: &runtimeConfig{
				enabledFeatures: wasm.FeatureSignExtensionOps,
			},
		},
		{
			name: "REC-wasm-core-1-20191205",
			with: func(c RuntimeConfig) RuntimeConfig {
				return c.WithFeatureSignExtensionOps(true).WithWasmCore1()
			},
			expected: &runtimeConfig{
				enabledFeatures: wasm.Features20191205,
			},
		},
		{
			name: "WD-wasm-core-2-20220419",
			with: func(c RuntimeConfig) RuntimeConfig {
				return c.WithFeatureMutableGlobal(false).WithWasmCore2()
			},
			expected: &runtimeConfig{
				enabledFeatures: wasm.Features20220419,
			},
		},
		{
			name: "reference-types",
			with: func(c RuntimeConfig) RuntimeConfig {
				return c.WithFeatureReferenceTypes(true)
			},
			expected: &runtimeConfig{
				enabledFeatures: wasm.FeatureBulkMemoryOperations | wasm.FeatureReferenceTypes,
			},
		},
	}
	for _, tt := range tests {
		tc := tt

		t.Run(tc.name, func(t *testing.T) {
			input := &runtimeConfig{}
			rc := tc.with(input)
			require.Equal(t, tc.expected, rc)
			// The source wasn't modified
			require.Equal(t, &runtimeConfig{}, input)
		})
	}

	t.Run("WithMemoryCapacityPages", func(t *testing.T) {
		c := NewRuntimeConfig().(*runtimeConfig)

		// Test default returns min
		require.Equal(t, uint32(1), c.memoryCapacityPages(1, nil))

		// Nil ignored
		c = c.WithMemoryCapacityPages(nil).(*runtimeConfig)
		require.Equal(t, uint32(1), c.memoryCapacityPages(1, nil))

		// Assign a valid function
		c = c.WithMemoryCapacityPages(func(minPages uint32, maxPages *uint32) uint32 {
			return 2
		}).(*runtimeConfig)
		// Returns updated value
		require.Equal(t, uint32(2), c.memoryCapacityPages(1, nil))
	})
}

func TestRuntimeConfig_FeatureToggle(t *testing.T) {
	tests := []struct {
		name          string
		feature       wasm.Features
		expectDefault bool
		setFeature    func(RuntimeConfig, bool) RuntimeConfig
	}{
		{
			name:          "bulk-memory-operations",
			feature:       wasm.FeatureBulkMemoryOperations,
			expectDefault: false,
			setFeature: func(c RuntimeConfig, v bool) RuntimeConfig {
				return c.WithFeatureBulkMemoryOperations(v)
			},
		},
		{
			name:          "multi-value",
			feature:       wasm.FeatureMultiValue,
			expectDefault: false,
			setFeature: func(c RuntimeConfig, v bool) RuntimeConfig {
				return c.WithFeatureMultiValue(v)
			},
		},
		{
			name:          "mutable-global",
			feature:       wasm.FeatureMutableGlobal,
			expectDefault: true,
			setFeature: func(c RuntimeConfig, v bool) RuntimeConfig {
				return c.WithFeatureMutableGlobal(v)
			},
		},
		{
			name:          "nontrapping-float-to-int-conversion",
			feature:       wasm.FeatureNonTrappingFloatToIntConversion,
			expectDefault: false,
			setFeature: func(c RuntimeConfig, v bool) RuntimeConfig {
				return c.WithFeatureNonTrappingFloatToIntConversion(v)
			},
		},
		{
			name:          "sign-extension-ops",
			feature:       wasm.FeatureSignExtensionOps,
			expectDefault: false,
			setFeature: func(c RuntimeConfig, v bool) RuntimeConfig {
				return c.WithFeatureSignExtensionOps(v)
			},
		},
	}

	for _, tt := range tests {
		tc := tt

		t.Run(tc.name, func(t *testing.T) {
			c := NewRuntimeConfig().(*runtimeConfig)
			require.Equal(t, tc.expectDefault, c.enabledFeatures.Get(tc.feature))

			// Set to false even if it was initially false.
			c = tc.setFeature(c, false).(*runtimeConfig)
			require.False(t, c.enabledFeatures.Get(tc.feature))

			// Set true makes it true
			c = tc.setFeature(c, true).(*runtimeConfig)
			require.True(t, c.enabledFeatures.Get(tc.feature))

			// Set false makes it false again
			c = tc.setFeature(c, false).(*runtimeConfig)
			require.False(t, c.enabledFeatures.Get(tc.feature))
		})
	}
}

func TestModuleConfig(t *testing.T) {
	tests := []struct {
		name     string
		with     func(ModuleConfig) ModuleConfig
		expected ModuleConfig
	}{
		{
			name: "WithName",
			with: func(c ModuleConfig) ModuleConfig {
				return c.WithName("wazero")
			},
			expected: &moduleConfig{
				name: "wazero",
			},
		},
		{
			name: "WithName - empty",
			with: func(c ModuleConfig) ModuleConfig {
				return c.WithName("")
			},
			expected: &moduleConfig{},
		},
		{
			name: "WithImport",
			with: func(c ModuleConfig) ModuleConfig {
				return c.WithImport("env", "abort", "assemblyscript", "abort")
			},
			expected: &moduleConfig{
				replacedImports: map[string][2]string{"env\000abort": {"assemblyscript", "abort"}},
			},
		},
		{
			name: "WithImport - empty to non-empty - module",
			with: func(c ModuleConfig) ModuleConfig {
				return c.WithImport("", "abort", "assemblyscript", "abort")
			},
			expected: &moduleConfig{
				replacedImports: map[string][2]string{"\000abort": {"assemblyscript", "abort"}},
			},
		},
		{
			name: "WithImport - non-empty to empty - module",
			with: func(c ModuleConfig) ModuleConfig {
				return c.WithImport("env", "abort", "", "abort")
			},
			expected: &moduleConfig{
				replacedImports: map[string][2]string{"env\000abort": {"", "abort"}},
			},
		},
		{
			name: "WithImport - empty to non-empty - name",
			with: func(c ModuleConfig) ModuleConfig {
				return c.WithImport("env", "", "assemblyscript", "abort")
			},
			expected: &moduleConfig{
				replacedImports: map[string][2]string{"env\000": {"assemblyscript", "abort"}},
			},
		},
		{
			name: "WithImport - non-empty to empty - name",
			with: func(c ModuleConfig) ModuleConfig {
				return c.WithImport("env", "abort", "assemblyscript", "")
			},
			expected: &moduleConfig{
				replacedImports: map[string][2]string{"env\000abort": {"assemblyscript", ""}},
			},
		},
		{
			name: "WithImport - override",
			with: func(c ModuleConfig) ModuleConfig {
				return c.WithImport("env", "abort", "assemblyscript", "abort").
					WithImport("env", "abort", "go", "exit")
			},
			expected: &moduleConfig{
				replacedImports: map[string][2]string{"env\000abort": {"go", "exit"}},
			},
		},
		{
			name: "WithImport - twice",
			with: func(c ModuleConfig) ModuleConfig {
				return c.WithImport("env", "abort", "assemblyscript", "abort").
					WithImport("wasi_unstable", "proc_exit", "wasi_snapshot_preview1", "proc_exit")
			},
			expected: &moduleConfig{
				replacedImports: map[string][2]string{
					"env\000abort":               {"assemblyscript", "abort"},
					"wasi_unstable\000proc_exit": {"wasi_snapshot_preview1", "proc_exit"},
				},
			},
		},
		{
			name: "WithImportModule",
			with: func(c ModuleConfig) ModuleConfig {
				return c.WithImportModule("env", "assemblyscript")
			},
			expected: &moduleConfig{
				replacedImportModules: map[string]string{"env": "assemblyscript"},
			},
		},
		{
			name: "WithImportModule - empty to non-empty",
			with: func(c ModuleConfig) ModuleConfig {
				return c.WithImportModule("", "assemblyscript")
			},
			expected: &moduleConfig{
				replacedImportModules: map[string]string{"": "assemblyscript"},
			},
		},
		{
			name: "WithImportModule - non-empty to empty",
			with: func(c ModuleConfig) ModuleConfig {
				return c.WithImportModule("env", "")
			},
			expected: &moduleConfig{
				replacedImportModules: map[string]string{"env": ""},
			},
		},
		{
			name: "WithImportModule - override",
			with: func(c ModuleConfig) ModuleConfig {
				return c.WithImportModule("env", "assemblyscript").
					WithImportModule("env", "go")
			},
			expected: &moduleConfig{
				replacedImportModules: map[string]string{"env": "go"},
			},
		},
		{
			name: "WithImportModule - twice",
			with: func(c ModuleConfig) ModuleConfig {
				return c.WithImportModule("env", "go").
					WithImportModule("wasi_unstable", "wasi_snapshot_preview1")
			},
			expected: &moduleConfig{
				replacedImportModules: map[string]string{
					"env":           "go",
					"wasi_unstable": "wasi_snapshot_preview1",
				},
			},
		},
	}
	for _, tt := range tests {
		tc := tt

		t.Run(tc.name, func(t *testing.T) {
			input := &moduleConfig{}
			rc := tc.with(input)
			require.Equal(t, tc.expected, rc)
			// The source wasn't modified
			require.Equal(t, &moduleConfig{}, input)
		})
	}
}

func TestModuleConfig_replaceImports(t *testing.T) {
	tests := []struct {
		name       string
		config     ModuleConfig
		input      *wasm.Module
		expected   *wasm.Module
		expectSame bool
	}{
		{
			name:       "no config, no imports",
			config:     &moduleConfig{},
			input:      &wasm.Module{},
			expected:   &wasm.Module{},
			expectSame: true,
		},
		{
			name:   "no config",
			config: &moduleConfig{},
			input: &wasm.Module{
				ImportSection: []*wasm.Import{
					{
						Module: "wasi_snapshot_preview1", Name: "args_sizes_get",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 0,
					},
					{
						Module: "wasi_snapshot_preview1", Name: "fd_write",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 2,
					},
				},
			},
			expectSame: true,
		},
		{
			name: "replacedImportModules",
			config: &moduleConfig{
				replacedImportModules: map[string]string{"wasi_unstable": "wasi_snapshot_preview1"},
			},
			input: &wasm.Module{
				ImportSection: []*wasm.Import{
					{
						Module: "wasi_unstable", Name: "args_sizes_get",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 0,
					},
					{
						Module: "wasi_unstable", Name: "fd_write",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 2,
					},
				},
			},
			expected: &wasm.Module{
				ImportSection: []*wasm.Import{
					{
						Module: "wasi_snapshot_preview1", Name: "args_sizes_get",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 0,
					},
					{
						Module: "wasi_snapshot_preview1", Name: "fd_write",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 2,
					},
				},
			},
		},
		{
			name: "replacedImportModules doesn't match",
			config: &moduleConfig{
				replacedImportModules: map[string]string{"env": ""},
			},
			input: &wasm.Module{
				ImportSection: []*wasm.Import{
					{
						Module: "wasi_snapshot_preview1", Name: "args_sizes_get",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 0,
					},
					{
						Module: "wasi_snapshot_preview1", Name: "fd_write",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 2,
					},
				},
			},
			expectSame: true,
		},
		{
			name: "replacedImports",
			config: &moduleConfig{
				replacedImports: map[string][2]string{"env\000abort": {"assemblyscript", "abort"}},
			},
			input: &wasm.Module{
				ImportSection: []*wasm.Import{
					{
						Module: "env", Name: "abort",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 0,
					},
					{
						Module: "env", Name: "seed",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 2,
					},
				},
			},
			expected: &wasm.Module{
				ImportSection: []*wasm.Import{
					{
						Module: "assemblyscript", Name: "abort",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 0,
					},
					{
						Module: "env", Name: "seed",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 2,
					},
				},
			},
		},
		{
			name: "replacedImports don't match",
			config: &moduleConfig{
				replacedImports: map[string][2]string{"env\000abort": {"assemblyscript", "abort"}},
			},
			input: &wasm.Module{
				ImportSection: []*wasm.Import{
					{
						Module: "wasi_snapshot_preview1", Name: "args_sizes_get",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 0,
					},
					{
						Module: "wasi_snapshot_preview1", Name: "fd_write",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 2,
					},
				},
			},
			expectSame: true,
		},
		{
			name: "replacedImportModules and replacedImports",
			config: &moduleConfig{
				replacedImportModules: map[string]string{"js": "wasm"},
				replacedImports: map[string][2]string{
					"wasm\000increment": {"go", "increment"},
					"wasm\000decrement": {"go", "decrement"},
				},
			},
			input: &wasm.Module{
				ImportSection: []*wasm.Import{
					{
						Module: "js", Name: "tbl",
						Type:      wasm.ExternTypeTable,
						DescTable: &wasm.Table{Min: 4},
					},
					{
						Module: "js", Name: "increment",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 0,
					},
					{
						Module: "js", Name: "decrement",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 0,
					},
					{
						Module: "js", Name: "wasm_increment",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 0,
					},
					{
						Module: "js", Name: "wasm_increment",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 0,
					},
				},
			},
			expected: &wasm.Module{
				ImportSection: []*wasm.Import{
					{
						Module: "wasm", Name: "tbl",
						Type:      wasm.ExternTypeTable,
						DescTable: &wasm.Table{Min: 4},
					},
					{
						Module: "go", Name: "increment",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 0,
					},
					{
						Module: "go", Name: "decrement",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 0,
					},
					{
						Module: "wasm", Name: "wasm_increment",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 0,
					},
					{
						Module: "wasm", Name: "wasm_increment",
						Type:     wasm.ExternTypeFunc,
						DescFunc: 0,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		tc := tt

		t.Run(tc.name, func(t *testing.T) {
			actual := tc.config.(*moduleConfig).replaceImports(tc.input)
			if tc.expectSame {
				require.Same(t, tc.input, actual)
			} else {
				require.NotSame(t, tc.input, actual)
				require.Equal(t, tc.expected, actual)
			}
		})
	}
}

func TestModuleConfig_toSysContext(t *testing.T) {
	testFS := fstest.MapFS{}
	testFS2 := fstest.MapFS{}

	tests := []struct {
		name     string
		input    ModuleConfig
		expected *wasm.SysContext
	}{
		{
			name:  "empty",
			input: NewModuleConfig(),
			expected: requireSysContext(t,
				math.MaxUint32, // max
				nil,            // args
				nil,            // environ
				nil,            // stdin
				nil,            // stdout
				nil,            // stderr
				nil,            // openedFiles
			),
		},
		{
			name:  "WithArgs",
			input: NewModuleConfig().WithArgs("a", "bc"),
			expected: requireSysContext(t,
				math.MaxUint32,      // max
				[]string{"a", "bc"}, // args
				nil,                 // environ
				nil,                 // stdin
				nil,                 // stdout
				nil,                 // stderr
				nil,                 // openedFiles
			),
		},
		{
			name:  "WithArgs - empty ok", // Particularly argv[0] can be empty, and we have no rules about others.
			input: NewModuleConfig().WithArgs("", "bc"),
			expected: requireSysContext(t,
				math.MaxUint32,     // max
				[]string{"", "bc"}, // args
				nil,                // environ
				nil,                // stdin
				nil,                // stdout
				nil,                // stderr
				nil,                // openedFiles
			),
		},
		{
			name:  "WithArgs - second call overwrites",
			input: NewModuleConfig().WithArgs("a", "bc").WithArgs("bc", "a"),
			expected: requireSysContext(t,
				math.MaxUint32,      // max
				[]string{"bc", "a"}, // args
				nil,                 // environ
				nil,                 // stdin
				nil,                 // stdout
				nil,                 // stderr
				nil,                 // openedFiles
			),
		},
		{
			name:  "WithEnv",
			input: NewModuleConfig().WithEnv("a", "b"),
			expected: requireSysContext(t,
				math.MaxUint32,  // max
				nil,             // args
				[]string{"a=b"}, // environ
				nil,             // stdin
				nil,             // stdout
				nil,             // stderr
				nil,             // openedFiles
			),
		},
		{
			name:  "WithEnv - empty value",
			input: NewModuleConfig().WithEnv("a", ""),
			expected: requireSysContext(t,
				math.MaxUint32, // max
				nil,            // args
				[]string{"a="}, // environ
				nil,            // stdin
				nil,            // stdout
				nil,            // stderr
				nil,            // openedFiles
			),
		},
		{
			name:  "WithEnv twice",
			input: NewModuleConfig().WithEnv("a", "b").WithEnv("c", "de"),
			expected: requireSysContext(t,
				math.MaxUint32,          // max
				nil,                     // args
				[]string{"a=b", "c=de"}, // environ
				nil,                     // stdin
				nil,                     // stdout
				nil,                     // stderr
				nil,                     // openedFiles
			),
		},
		{
			name:  "WithEnv overwrites",
			input: NewModuleConfig().WithEnv("a", "bc").WithEnv("c", "de").WithEnv("a", "de"),
			expected: requireSysContext(t,
				math.MaxUint32,           // max
				nil,                      // args
				[]string{"a=de", "c=de"}, // environ
				nil,                      // stdin
				nil,                      // stdout
				nil,                      // stderr
				nil,                      // openedFiles
			),
		},

		{
			name:  "WithEnv twice",
			input: NewModuleConfig().WithEnv("a", "b").WithEnv("c", "de"),
			expected: requireSysContext(t,
				math.MaxUint32,          // max
				nil,                     // args
				[]string{"a=b", "c=de"}, // environ
				nil,                     // stdin
				nil,                     // stdout
				nil,                     // stderr
				nil,                     // openedFiles
			),
		},
		{
			name:  "WithFS",
			input: NewModuleConfig().WithFS(testFS),
			expected: requireSysContext(t,
				math.MaxUint32, // max
				nil,            // args
				nil,            // environ
				nil,            // stdin
				nil,            // stdout
				nil,            // stderr
				map[uint32]*wasm.FileEntry{ // openedFiles
					3: {Path: "/", FS: testFS},
					4: {Path: ".", FS: testFS},
				},
			),
		},
		{
			name:  "WithFS - overwrites",
			input: NewModuleConfig().WithFS(testFS).WithFS(testFS2),
			expected: requireSysContext(t,
				math.MaxUint32, // max
				nil,            // args
				nil,            // environ
				nil,            // stdin
				nil,            // stdout
				nil,            // stderr
				map[uint32]*wasm.FileEntry{ // openedFiles
					3: {Path: "/", FS: testFS2},
					4: {Path: ".", FS: testFS2},
				},
			),
		},
		{
			name:  "WithWorkDirFS",
			input: NewModuleConfig().WithWorkDirFS(testFS),
			expected: requireSysContext(t,
				math.MaxUint32, // max
				nil,            // args
				nil,            // environ
				nil,            // stdin
				nil,            // stdout
				nil,            // stderr
				map[uint32]*wasm.FileEntry{ // openedFiles
					3: {Path: ".", FS: testFS},
				},
			),
		},
		{
			name:  "WithFS and WithWorkDirFS",
			input: NewModuleConfig().WithFS(testFS).WithWorkDirFS(testFS2),
			expected: requireSysContext(t,
				math.MaxUint32, // max
				nil,            // args
				nil,            // environ
				nil,            // stdin
				nil,            // stdout
				nil,            // stderr
				map[uint32]*wasm.FileEntry{ // openedFiles
					3: {Path: "/", FS: testFS},
					4: {Path: ".", FS: testFS2},
				},
			),
		},
		{
			name:  "WithWorkDirFS and WithFS",
			input: NewModuleConfig().WithWorkDirFS(testFS).WithFS(testFS2),
			expected: requireSysContext(t,
				math.MaxUint32, // max
				nil,            // args
				nil,            // environ
				nil,            // stdin
				nil,            // stdout
				nil,            // stderr
				map[uint32]*wasm.FileEntry{ // openedFiles
					3: {Path: ".", FS: testFS},
					4: {Path: "/", FS: testFS2},
				},
			),
		},
	}
	for _, tt := range tests {
		tc := tt

		t.Run(tc.name, func(t *testing.T) {
			sys, err := tc.input.(*moduleConfig).toSysContext()
			require.NoError(t, err)
			require.Equal(t, tc.expected, sys)
		})
	}
}

func TestModuleConfig_toSysContext_Errors(t *testing.T) {
	tests := []struct {
		name        string
		input       ModuleConfig
		expectedErr string
	}{
		{
			name:        "WithArgs - arg contains NUL",
			input:       NewModuleConfig().WithArgs("", string([]byte{'a', 0})),
			expectedErr: "args invalid: contains NUL character",
		},
		{
			name:        "WithEnv - key contains NUL",
			input:       NewModuleConfig().WithEnv(string([]byte{'a', 0}), "a"),
			expectedErr: "environ invalid: contains NUL character",
		},
		{
			name:        "WithEnv - value contains NUL",
			input:       NewModuleConfig().WithEnv("a", string([]byte{'a', 0})),
			expectedErr: "environ invalid: contains NUL character",
		},
		{
			name:        "WithEnv - key contains equals",
			input:       NewModuleConfig().WithEnv("a=", "a"),
			expectedErr: "environ invalid: key contains '=' character",
		},
		{
			name:        "WithEnv - empty key",
			input:       NewModuleConfig().WithEnv("", "a"),
			expectedErr: "environ invalid: empty key",
		},
		{
			name:        "WithFS - nil",
			input:       NewModuleConfig().WithFS(nil),
			expectedErr: "FS for / is nil",
		},
		{
			name:        "WithWorkDirFS - nil",
			input:       NewModuleConfig().WithWorkDirFS(nil),
			expectedErr: "FS for . is nil",
		},
	}
	for _, tt := range tests {
		tc := tt

		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.input.(*moduleConfig).toSysContext()
			require.EqualError(t, err, tc.expectedErr)
		})
	}
}

// requireSysContext ensures wasm.NewSysContext doesn't return an error, which makes it usable in test matrices.
func requireSysContext(t *testing.T, max uint32, args, environ []string, stdin io.Reader, stdout, stderr io.Writer, openedFiles map[uint32]*wasm.FileEntry) *wasm.SysContext {
	sys, err := wasm.NewSysContext(max, args, environ, stdin, stdout, stderr, openedFiles)
	require.NoError(t, err)
	return sys
}

func TestCompiledCode_Close(t *testing.T) {
	for _, ctx := range []context.Context{nil, testCtx} { // Ensure it doesn't crash on nil!
		e := &mockEngine{name: "1", cachedModules: map[*wasm.Module]struct{}{}}

		var cs []*compiledCode
		for i := 0; i < 10; i++ {
			m := &wasm.Module{}
			err := e.CompileModule(ctx, m)
			require.NoError(t, err)
			cs = append(cs, &compiledCode{module: m, compiledEngine: e})
		}

		// Before Close.
		require.Equal(t, 10, len(e.cachedModules))

		for _, c := range cs {
			require.NoError(t, c.Close(ctx))
		}

		// After Close.
		require.Zero(t, len(e.cachedModules))
	}
}

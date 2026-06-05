package ui

import (
	"strings"
	"testing"

	"github.com/ultramcu/yon/internal/model"
)

// FAIL-BEFORE: at authoring time the ui package had no collectVariableView,
// renderVarLine, or varView symbol, so this file would not compile against the
// prior tree (undefined identifiers) — that compile failure is the fail-before.
// PASS-AFTER: with varspanel.go present these assert the collector's
// precedence/dedupe/scope/secret-flag, the deterministic runtime sort, the
// no-env case, and the renderer's secret masking (value never leaked).

func TestCollectVariableView_PrecedenceDedupeScope(t *testing.T) {
	env := model.Environment{
		Name: "Local",
		Variables: []model.Variable{
			{Key: "baseUrl", Value: "http://localhost:7878", Enabled: true},
			{Key: "token", Value: "env-token", Enabled: true},
			{Key: "disabledEnv", Value: "x", Enabled: false}, // dropped
			{Key: "apiKey", Value: "supersecret", Enabled: true, Secret: true},
		},
	}
	coll := []model.Variable{
		{Key: "token", Value: "coll-token", Enabled: true},    // clash → env wins, dropped
		{Key: "collOnly", Value: "coll-value", Enabled: true}, // kept, scope collection
		{Key: "disabledColl", Value: "y", Enabled: false},     // dropped
	}

	configured, _ := collectVariableView(env, coll, nil)

	// Expect, in order: env baseUrl, env token, env apiKey, collection collOnly.
	wantKeys := []string{"baseUrl", "token", "apiKey", "collOnly"}
	if len(configured) != len(wantKeys) {
		t.Fatalf("configured len = %d, want %d: %+v", len(configured), len(wantKeys), configured)
	}
	for i, k := range wantKeys {
		if configured[i].Key != k {
			t.Fatalf("configured[%d].Key = %q, want %q (order: %+v)", i, configured[i].Key, k, configured)
		}
	}

	// Scopes: first three env, last collection.
	for i := 0; i < 3; i++ {
		if configured[i].Scope != scopeEnv {
			t.Errorf("configured[%d].Scope = %q, want %q", i, configured[i].Scope, scopeEnv)
		}
	}
	if configured[3].Scope != scopeCollection {
		t.Errorf("collOnly Scope = %q, want %q", configured[3].Scope, scopeCollection)
	}

	// Env wins on the clashing key: token carries the ENV value, not the coll one.
	if configured[1].Value != "env-token" {
		t.Errorf("token Value = %q, want env-token (env must win clash)", configured[1].Value)
	}

	// Secret flag is carried verbatim by the collector (masking is the renderer's job).
	if !configured[2].Secret {
		t.Errorf("apiKey Secret flag lost: %+v", configured[2])
	}
	if configured[0].Secret {
		t.Errorf("baseUrl wrongly marked Secret: %+v", configured[0])
	}
}

func TestCollectVariableView_RuntimeSortedAndScoped(t *testing.T) {
	runtime := map[string]string{
		"zebra": "z",
		"alpha": "a",
		"mike":  "m",
	}
	_, runtimeRows := collectVariableView(model.Environment{}, nil, runtime)

	wantOrder := []string{"alpha", "mike", "zebra"} // sorted by Key
	if len(runtimeRows) != len(wantOrder) {
		t.Fatalf("runtimeRows len = %d, want %d", len(runtimeRows), len(wantOrder))
	}
	for i, k := range wantOrder {
		if runtimeRows[i].Key != k {
			t.Fatalf("runtimeRows[%d].Key = %q, want %q (not sorted: %+v)", i, runtimeRows[i].Key, k, runtimeRows)
		}
		if runtimeRows[i].Scope != scopeRuntime {
			t.Errorf("runtimeRows[%d].Scope = %q, want %q", i, runtimeRows[i].Scope, scopeRuntime)
		}
		if runtimeRows[i].Secret {
			t.Errorf("runtime row %q must never be Secret", runtimeRows[i].Key)
		}
	}
}

func TestCollectVariableView_NoEnvAndNilRuntime(t *testing.T) {
	// Zero env contributes no env rows; only enabled collection vars appear.
	coll := []model.Variable{
		{Key: "collOnly", Value: "v", Enabled: true},
	}
	configured, runtimeRows := collectVariableView(model.Environment{}, coll, nil)

	if len(configured) != 1 || configured[0].Key != "collOnly" || configured[0].Scope != scopeCollection {
		t.Fatalf("no-env configured = %+v, want one collection row", configured)
	}
	if len(runtimeRows) != 0 {
		t.Fatalf("nil runtime must yield empty runtimeRows, got %+v", runtimeRows)
	}
}

func TestRenderVarLine_PlainAndSecretMasked(t *testing.T) {
	plain := renderVarLine(varView{Key: "baseUrl", Value: "http://localhost:7878", Scope: scopeEnv})
	if plain != "baseUrl = http://localhost:7878" {
		t.Errorf("plain line = %q, want %q", plain, "baseUrl = http://localhost:7878")
	}

	const secretValue = "supersecret-leak-me"
	secret := renderVarLine(varView{Key: "apiKey", Value: secretValue, Secret: true, Scope: scopeEnv})

	// The secret VALUE must NOT appear anywhere in the rendered line.
	if strings.Contains(secret, secretValue) {
		t.Fatalf("secret value leaked in render output: %q", secret)
	}
	if secret != "apiKey = "+secretMask {
		t.Errorf("secret line = %q, want %q", secret, "apiKey = "+secretMask)
	}
}

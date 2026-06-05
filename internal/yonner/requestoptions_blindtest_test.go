package yonner

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ultramcu/yon/internal/model"
)

// ---------------------------------------------------------------------------
// helpers (blind: only construct from contract symbols)
// ---------------------------------------------------------------------------

func intPtr(n int) *int    { return &n }
func boolPtr(b bool) *bool { return &b }

// ===========================================================================
// A. TestApplyRequestOptions
//
// Contract: ApplyRequestOptions(base Options, ro *model.RequestOptions) Options
// overlays non-nil override fields onto base; nil ro or nil field leaves base
// unchanged. TimeoutSeconds maps to base.Timeout = n*time.Second (0 => no
// client timeout). Resolve/DialContext untouched.
// ===========================================================================

func TestApplyRequestOptions(t *testing.T) {
	// --- nil ro -> base returned unchanged --------------------------------
	t.Run("NilROLeavesBaseUnchanged", func(t *testing.T) {
		base := Options{
			Timeout:         30 * time.Second,
			InsecureTLS:     true,
			FollowRedirects: false,
		}
		got := ApplyRequestOptions(base, nil)
		if got.Timeout != base.Timeout {
			t.Errorf("Timeout: got %v, want %v (unchanged)", got.Timeout, base.Timeout)
		}
		if got.InsecureTLS != base.InsecureTLS {
			t.Errorf("InsecureTLS: got %v, want %v (unchanged)", got.InsecureTLS, base.InsecureTLS)
		}
		if got.FollowRedirects != base.FollowRedirects {
			t.Errorf("FollowRedirects: got %v, want %v (unchanged)", got.FollowRedirects, base.FollowRedirects)
		}
	})

	// --- empty ro (all fields nil) -> base unchanged ----------------------
	t.Run("EmptyROLeavesBaseUnchanged", func(t *testing.T) {
		base := Options{
			Timeout:         15 * time.Second,
			InsecureTLS:     true,
			FollowRedirects: true,
		}
		got := ApplyRequestOptions(base, &model.RequestOptions{})
		if got.Timeout != base.Timeout {
			t.Errorf("Timeout: got %v, want %v (unchanged)", got.Timeout, base.Timeout)
		}
		if got.InsecureTLS != base.InsecureTLS {
			t.Errorf("InsecureTLS: got %v, want %v (unchanged)", got.InsecureTLS, base.InsecureTLS)
		}
		if got.FollowRedirects != base.FollowRedirects {
			t.Errorf("FollowRedirects: got %v, want %v (unchanged)", got.FollowRedirects, base.FollowRedirects)
		}
	})

	// --- only InsecureTLS set: true over base{false} ----------------------
	t.Run("OnlyInsecureTLSOverridden", func(t *testing.T) {
		base := Options{
			Timeout:         30 * time.Second,
			InsecureTLS:     false,
			FollowRedirects: true,
		}
		got := ApplyRequestOptions(base, &model.RequestOptions{InsecureTLS: boolPtr(true)})
		if !got.InsecureTLS {
			t.Errorf("InsecureTLS: got %v, want true (overridden)", got.InsecureTLS)
		}
		if got.Timeout != base.Timeout {
			t.Errorf("Timeout: got %v, want %v (unchanged)", got.Timeout, base.Timeout)
		}
		if got.FollowRedirects != base.FollowRedirects {
			t.Errorf("FollowRedirects: got %v, want %v (unchanged)", got.FollowRedirects, base.FollowRedirects)
		}
	})

	// --- the KEY case: FollowRedirects=false explicitly overrides a truthy
	// global. This is exactly the bug a naive impl that only ever flips a
	// flag "on" would miss.
	t.Run("FollowRedirectsFalseOverridesTruthyGlobal", func(t *testing.T) {
		base := Options{
			Timeout:         30 * time.Second,
			InsecureTLS:     false,
			FollowRedirects: true,
		}
		got := ApplyRequestOptions(base, &model.RequestOptions{FollowRedirects: boolPtr(false)})
		if got.FollowRedirects {
			t.Errorf("FollowRedirects: got %v, want false (explicit override of truthy global)", got.FollowRedirects)
		}
		if got.Timeout != base.Timeout {
			t.Errorf("Timeout: got %v, want %v (unchanged)", got.Timeout, base.Timeout)
		}
		if got.InsecureTLS != base.InsecureTLS {
			t.Errorf("InsecureTLS: got %v, want %v (unchanged)", got.InsecureTLS, base.InsecureTLS)
		}
	})

	// --- symmetric: InsecureTLS=false overrides a truthy global -----------
	t.Run("InsecureTLSFalseOverridesTruthyGlobal", func(t *testing.T) {
		base := Options{
			Timeout:         30 * time.Second,
			InsecureTLS:     true,
			FollowRedirects: true,
		}
		got := ApplyRequestOptions(base, &model.RequestOptions{InsecureTLS: boolPtr(false)})
		if got.InsecureTLS {
			t.Errorf("InsecureTLS: got %v, want false (explicit override of truthy global)", got.InsecureTLS)
		}
	})

	// --- TimeoutSeconds=30 -> base.Timeout == 30s -------------------------
	t.Run("TimeoutSecondsMapsToDuration", func(t *testing.T) {
		base := Options{
			Timeout:         5 * time.Second,
			InsecureTLS:     false,
			FollowRedirects: true,
		}
		got := ApplyRequestOptions(base, &model.RequestOptions{TimeoutSeconds: intPtr(30)})
		if got.Timeout != 30*time.Second {
			t.Errorf("Timeout: got %v, want %v", got.Timeout, 30*time.Second)
		}
		if got.InsecureTLS != base.InsecureTLS {
			t.Errorf("InsecureTLS: got %v, want %v (unchanged)", got.InsecureTLS, base.InsecureTLS)
		}
		if got.FollowRedirects != base.FollowRedirects {
			t.Errorf("FollowRedirects: got %v, want %v (unchanged)", got.FollowRedirects, base.FollowRedirects)
		}
	})

	// --- TimeoutSeconds=0 -> base.Timeout == 0 (no client timeout) --------
	// The other key case: an explicit zero must be applied, not treated as
	// "unset" and skipped (which would leave the non-zero base timeout in).
	t.Run("TimeoutSecondsZeroMeansNoTimeout", func(t *testing.T) {
		base := Options{
			Timeout:         30 * time.Second,
			InsecureTLS:     false,
			FollowRedirects: true,
		}
		got := ApplyRequestOptions(base, &model.RequestOptions{TimeoutSeconds: intPtr(0)})
		if got.Timeout != 0 {
			t.Errorf("Timeout: got %v, want 0 (explicit no-timeout override)", got.Timeout)
		}
	})

	// --- all three set -> all overridden ----------------------------------
	t.Run("AllThreeOverridden", func(t *testing.T) {
		base := Options{
			Timeout:         30 * time.Second,
			InsecureTLS:     false,
			FollowRedirects: true,
		}
		ro := &model.RequestOptions{
			TimeoutSeconds:  intPtr(7),
			InsecureTLS:     boolPtr(true),
			FollowRedirects: boolPtr(false),
		}
		got := ApplyRequestOptions(base, ro)
		if got.Timeout != 7*time.Second {
			t.Errorf("Timeout: got %v, want %v", got.Timeout, 7*time.Second)
		}
		if !got.InsecureTLS {
			t.Errorf("InsecureTLS: got %v, want true", got.InsecureTLS)
		}
		if got.FollowRedirects {
			t.Errorf("FollowRedirects: got %v, want false", got.FollowRedirects)
		}
	})

	// --- base is not mutated in place -------------------------------------
	t.Run("BaseNotMutated", func(t *testing.T) {
		base := Options{
			Timeout:         30 * time.Second,
			InsecureTLS:     false,
			FollowRedirects: true,
		}
		ro := &model.RequestOptions{
			TimeoutSeconds:  intPtr(1),
			InsecureTLS:     boolPtr(true),
			FollowRedirects: boolPtr(false),
		}
		_ = ApplyRequestOptions(base, ro)
		if base.Timeout != 30*time.Second || base.InsecureTLS || !base.FollowRedirects {
			t.Errorf("base was mutated: %+v", base)
		}
	})
}

// ===========================================================================
// B. TestRequestOptionsJSONRoundTrip
//
// Contract: Request.Options *model.RequestOptions `json:"options,omitempty"`
// is absent in JSON when nil (backward compat); a set Options marshals then
// unmarshals back equal (deref-compare set pointers; unset fields stay nil).
// ===========================================================================

func TestRequestOptionsJSONRoundTrip(t *testing.T) {
	// --- nil Options -> "options" absent from JSON ------------------------
	t.Run("NilOptionsOmittedFromJSON", func(t *testing.T) {
		req := model.Request{
			Method: model.MethodGet,
			URL:    "http://example.com/",
			// Options left nil.
		}
		b, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if strings.Contains(string(b), "options") {
			t.Errorf("nil Options should be omitted from JSON (backward compat), got: %s", string(b))
		}
	})

	// --- set Options round-trips equal ------------------------------------
	t.Run("SetOptionsRoundTripsEqual", func(t *testing.T) {
		req := model.Request{
			Method: model.MethodPost,
			URL:    "http://example.com/",
			Options: &model.RequestOptions{
				TimeoutSeconds:  intPtr(5),
				FollowRedirects: boolPtr(false),
				// InsecureTLS intentionally left nil.
			},
		}
		b, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		// Sanity: the key is present this time.
		if !strings.Contains(string(b), "options") {
			t.Errorf("set Options should appear in JSON, got: %s", string(b))
		}

		var back model.Request
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if back.Options == nil {
			t.Fatalf("round-trip lost Options (nil after unmarshal); json=%s", string(b))
		}

		// TimeoutSeconds set -> deref-compare.
		if back.Options.TimeoutSeconds == nil {
			t.Errorf("TimeoutSeconds: got nil, want pointer to 5")
		} else if *back.Options.TimeoutSeconds != 5 {
			t.Errorf("TimeoutSeconds: got %d, want 5", *back.Options.TimeoutSeconds)
		}

		// FollowRedirects set to false -> must survive as non-nil false.
		if back.Options.FollowRedirects == nil {
			t.Errorf("FollowRedirects: got nil, want pointer to false")
		} else if *back.Options.FollowRedirects {
			t.Errorf("FollowRedirects: got %v, want false", *back.Options.FollowRedirects)
		}

		// InsecureTLS was unset -> stays nil through the round-trip.
		if back.Options.InsecureTLS != nil {
			t.Errorf("InsecureTLS: got %v, want nil (was unset)", *back.Options.InsecureTLS)
		}
	})
}

package auth

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

const (
	codexAdaptiveStrategy                = "adaptive"
	codexAdaptiveDefaultConcurrency      = 2
	codexAdaptiveMaxConcurrency          = 4
	codexAdaptiveQuotaStaleAfter         = 15 * time.Minute
	codexAdaptiveQuotaProbeInterval      = 5 * time.Minute
	codexAdaptiveProbeFailureBackoff     = 30 * time.Second
	codexAdaptiveLeaseTTL                = 10 * time.Minute
	codexAdaptiveWaitInterval            = 100 * time.Millisecond
	codexAdaptiveSuccessesBeforeIncrease = 8
	adaptiveExecutionMetadataKey         = "codex_adaptive_execution"
	adaptiveLeaseMetadataKey             = "codex_adaptive_lease"
)

type codexAdaptiveAccount struct {
	inFlight      int
	limit         int
	successes     int
	lastProbeAt   time.Time
	probeFailedAt time.Time
	probeInFlight bool
}

type codexAdaptiveLease struct {
	authID    string
	expiresAt time.Time
	done      chan struct{}
}

type codexAdaptiveRouter struct {
	mu       sync.Mutex
	accounts map[string]*codexAdaptiveAccount
	leases   map[string]codexAdaptiveLease
	changed  chan struct{}
}

func newCodexAdaptiveRouter() *codexAdaptiveRouter {
	return &codexAdaptiveRouter{
		accounts: make(map[string]*codexAdaptiveAccount),
		leases:   make(map[string]codexAdaptiveLease),
		changed:  make(chan struct{}),
	}
}

func (r *codexAdaptiveRouter) accountLocked(id string) *codexAdaptiveAccount {
	state := r.accounts[id]
	if state == nil {
		state = &codexAdaptiveAccount{limit: codexAdaptiveDefaultConcurrency}
		r.accounts[id] = state
	}
	return state
}

func (r *codexAdaptiveRouter) notifyLocked() {
	close(r.changed)
	r.changed = make(chan struct{})
}

func (r *codexAdaptiveRouter) pruneLeasesLocked(now time.Time) {
	for leaseID, lease := range r.leases {
		if lease.expiresAt.After(now) {
			continue
		}
		state := r.accountLocked(lease.authID)
		if state.inFlight > 0 {
			state.inFlight--
		}
		delete(r.leases, leaseID)
		close(lease.done)
	}
}

func (r *codexAdaptiveRouter) pick(ctx context.Context, candidates []*Auth, preferFree bool) (*Auth, string, error) {
	if r == nil || len(candidates) == 0 {
		return nil, "", &Error{Code: "auth_unavailable", Message: "no auth available"}
	}
	for {
		now := time.Now()
		selected := r.best(candidates, preferFree, now)
		if selected == nil {
			if errContext := contextError(ctx); errContext != nil {
				return nil, "", errContext
			}
			r.mu.Lock()
			changed := r.changed
			r.mu.Unlock()
			timer := time.NewTimer(codexAdaptiveWaitInterval)
			select {
			case <-changed:
				if !timer.Stop() {
					<-timer.C
				}
			case <-timer.C:
			case <-ctxDone(ctx):
				return nil, "", ctx.Err()
			}
			continue
		}

		r.mu.Lock()
		r.pruneLeasesLocked(now)
		state := r.accountLocked(selected.ID)
		if state.inFlight >= state.limit {
			r.mu.Unlock()
			continue
		}
		state.inFlight++
		leaseID := fmt.Sprintf("%s:%d", selected.ID, time.Now().UnixNano())
		done := make(chan struct{})
		r.leases[leaseID] = codexAdaptiveLease{authID: selected.ID, expiresAt: now.Add(codexAdaptiveLeaseTTL), done: done}
		r.mu.Unlock()
		r.renewLeaseUntilDone(ctx, leaseID, done)
		return selected, leaseID, nil
	}
}

func (r *codexAdaptiveRouter) renewLeaseUntilDone(ctx context.Context, leaseID string, done <-chan struct{}) {
	if r == nil || leaseID == "" {
		return
	}
	interval := codexAdaptiveLeaseTTL / 2
	if interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.mu.Lock()
				lease, ok := r.leases[leaseID]
				if ok {
					lease.expiresAt = time.Now().Add(codexAdaptiveLeaseTTL)
					r.leases[leaseID] = lease
				}
				r.mu.Unlock()
				if !ok {
					return
				}
			case <-ctxDone(ctx):
				r.release(leaseID)
				return
			case <-done:
				return
			}
		}
	}()
}

func adaptiveLeaseID(options cliproxyexecutor.Options) string {
	if options.Metadata == nil {
		return ""
	}
	if enabled, ok := options.Metadata[adaptiveExecutionMetadataKey].(bool); !ok || !enabled {
		return ""
	}
	leaseID, _ := options.Metadata[adaptiveLeaseMetadataKey].(string)
	return strings.TrimSpace(leaseID)
}

func markAdaptiveLease(options cliproxyexecutor.Options, leaseID string) {
	if leaseID == "" {
		return
	}
	metadata := options.EnsureMetadata()
	metadata[adaptiveExecutionMetadataKey] = true
	metadata[adaptiveLeaseMetadataKey] = leaseID
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func ctxDone(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}

func (r *codexAdaptiveRouter) best(candidates []*Auth, preferFree bool, now time.Time) *Auth {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLeasesLocked(now)

	freeAvailable := false
	if preferFree {
		for _, candidate := range candidates {
			if candidate == nil || !isFreeCodexAuth(candidate) {
				continue
			}
			state := r.accountLocked(candidate.ID)
			if state.inFlight < state.limit {
				freeAvailable = true
				break
			}
		}
	}

	var selected *Auth
	var selectedScore adaptiveScore
	for _, candidate := range candidates {
		if candidate == nil || candidate.ID == "" {
			continue
		}
		if freeAvailable && !isFreeCodexAuth(candidate) {
			continue
		}
		state := r.accountLocked(candidate.ID)
		if state.inFlight >= state.limit {
			continue
		}
		score := adaptiveScoreFor(candidate, state, now)
		if selected == nil || score.betterThan(selectedScore) {
			selected = candidate
			selectedScore = score
		}
	}
	return selected
}

type adaptiveScore struct {
	priority int
	urgency  float64
	load     int
	weight   int64
	id       string
}

func (s adaptiveScore) betterThan(other adaptiveScore) bool {
	if s.priority != other.priority {
		return s.priority > other.priority
	}
	if math.Abs(s.urgency-other.urgency) > 0.000001 {
		return s.urgency > other.urgency
	}
	if s.load != other.load {
		return s.load < other.load
	}
	if s.weight != other.weight {
		return s.weight > other.weight
	}
	return s.id < other.id
}

func adaptiveScoreFor(auth *Auth, state *codexAdaptiveAccount, now time.Time) adaptiveScore {
	return adaptiveScore{
		priority: authPriority(auth),
		urgency:  codexQuotaUrgency(auth, now),
		load:     state.inFlight,
		weight:   authWeight(auth),
		id:       auth.ID,
	}
}

func quotaProbeNeeded(auth *Auth, state *codexAdaptiveAccount, now time.Time) bool {
	if auth == nil || isFreeCodexAuth(auth) {
		return false
	}
	if !state.lastProbeAt.IsZero() && now.Sub(state.lastProbeAt) < codexAdaptiveQuotaProbeInterval {
		return false
	}
	if !state.probeFailedAt.IsZero() && now.Sub(state.probeFailedAt) < codexAdaptiveProbeFailureBackoff {
		return false
	}
	return auth.Quota.ObservedAt.IsZero() || now.Sub(auth.Quota.ObservedAt) >= codexAdaptiveQuotaStaleAfter
}

func codexQuotaUrgency(auth *Auth, now time.Time) float64 {
	if auth == nil || isFreeCodexAuth(auth) {
		return 0
	}
	weeklyPrefix := codexWeeklyQuotaPrefix(auth.Quota.Signals)
	remaining := 100 - quotaPercent(auth.Quota.Signals, weeklyPrefix+"Used-Percent")
	if remaining < 0 {
		remaining = 0
	}
	resetCredits := codexAvailableResetCredits(auth)
	if remaining == 0 && resetCredits > 0 {
		// A reset credit is only useful if this account reaches the reset point
		// before the credit expires, so exhausted accounts with credits remain urgent.
		remaining = 1
	}
	remaining *= 1 + resetCredits
	deadline := codexQuotaDeadline(auth, now, weeklyPrefix)
	if deadline.IsZero() {
		return float64(remaining) / 168
	}
	hours := deadline.Sub(now).Hours()
	if hours < 1.0/24.0 {
		hours = 1.0 / 24.0
	}
	return float64(remaining) / hours
}

func codexWeeklyQuotaPrefix(signals map[string]string) string {
	for _, prefix := range []string{"X-Codex-Primary-", "X-Codex-Secondary-"} {
		if minutes, err := strconv.Atoi(strings.TrimSpace(signals[prefix+"Window-Minutes"])); err == nil && minutes >= 10080 {
			return prefix
		}
	}
	if signals["X-Codex-Primary-Used-Percent"] != "" {
		return "X-Codex-Primary-"
	}
	return "X-Codex-Secondary-"
}

func codexQuotaDeadline(auth *Auth, now time.Time, weeklyPrefix string) time.Time {
	var deadline time.Time
	if auth == nil {
		return deadline
	}
	for _, key := range []string{"chatgpt_subscription_active_until", "subscription_active_until"} {
		if auth.Metadata == nil {
			break
		}
		if parsed, ok := parseTimeValue(auth.Metadata[key]); ok && (deadline.IsZero() || parsed.Before(deadline)) {
			deadline = parsed
		}
	}
	if resetCreditExpiry := codexResetCreditExpiry(auth); !resetCreditExpiry.IsZero() && (deadline.IsZero() || resetCreditExpiry.Before(deadline)) {
		deadline = resetCreditExpiry
	}
	if resetAt := quotaResetAt(auth.Quota.Signals, weeklyPrefix+"Reset-At", weeklyPrefix+"Reset-After-Seconds", auth.Quota.ObservedAt, now); !resetAt.IsZero() && (deadline.IsZero() || resetAt.Before(deadline)) {
		deadline = resetAt
	}
	return deadline
}

func codexResetCreditExpiry(auth *Auth) time.Time {
	if auth == nil || auth.Metadata == nil {
		return time.Time{}
	}
	raw, ok := auth.Metadata["rate_limit_reset_credits"]
	if !ok {
		return time.Time{}
	}
	var earliest time.Time
	consider := func(item any) {
		values, ok := item.(map[string]any)
		if !ok {
			return
		}
		if status, _ := values["status"].(string); status != "" && !strings.EqualFold(strings.TrimSpace(status), "available") {
			return
		}
		for _, key := range []string{"expires_at", "expiresAt"} {
			if expiry, ok := parseTimeValue(values[key]); ok && (earliest.IsZero() || expiry.Before(earliest)) {
				earliest = expiry
			}
		}
	}
	switch values := raw.(type) {
	case []any:
		for _, item := range values {
			consider(item)
		}
	case []map[string]any:
		for _, item := range values {
			consider(item)
		}
	}
	return earliest
}

func codexAvailableResetCredits(auth *Auth) int {
	if auth == nil || auth.Metadata == nil {
		return 0
	}
	for _, key := range []string{"rate_limit_reset_credits_applicable_available_count", "rate_limit_reset_credits_available_count"} {
		switch value := auth.Metadata[key].(type) {
		case int:
			if value > 0 {
				return min(value, 2)
			}
		case int64:
			if value > 0 {
				return min(int(value), 2)
			}
		case float64:
			if value > 0 {
				return min(int(value), 2)
			}
		case string:
			if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && parsed > 0 {
				return min(parsed, 2)
			}
		}
	}
	return 0
}

func metadataString(auth *Auth, keys ...string) string {
	if auth == nil || auth.Metadata == nil {
		return ""
	}
	for _, key := range keys {
		value, ok := auth.Metadata[key]
		if !ok || value == nil {
			continue
		}
		if text, ok := value.(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func quotaPercent(signals map[string]string, key string) int {
	value, err := strconv.Atoi(strings.TrimSpace(signals[key]))
	if err != nil || value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func quotaResetAt(signals map[string]string, atKey, afterKey string, observedAt, now time.Time) time.Time {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(signals[atKey]), 10, 64); err == nil && seconds > 0 {
		reset := time.Unix(seconds, 0)
		if reset.After(now) {
			return reset
		}
	}
	if seconds, err := strconv.ParseInt(strings.TrimSpace(signals[afterKey]), 10, 64); err == nil && seconds > 0 && !observedAt.IsZero() {
		reset := observedAt.Add(time.Duration(seconds) * time.Second)
		if reset.After(now) {
			return reset
		}
	}
	return time.Time{}
}

func (r *codexAdaptiveRouter) beginProbe(authID string, now time.Time) bool {
	if r == nil || authID == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := r.accountLocked(authID)
	if state.probeInFlight || (!state.lastProbeAt.IsZero() && now.Sub(state.lastProbeAt) < codexAdaptiveQuotaProbeInterval) ||
		(!state.probeFailedAt.IsZero() && now.Sub(state.probeFailedAt) < codexAdaptiveProbeFailureBackoff) {
		return false
	}
	state.probeInFlight = true
	state.lastProbeAt = now
	return true
}

func (r *codexAdaptiveRouter) finishProbe(authID string, success bool) {
	if r == nil || authID == "" {
		return
	}
	r.mu.Lock()
	state := r.accountLocked(authID)
	state.probeInFlight = false
	if success {
		state.lastProbeAt = time.Now()
		state.probeFailedAt = time.Time{}
	} else {
		state.lastProbeAt = time.Time{}
		state.probeFailedAt = time.Now()
	}
	r.notifyLocked()
	r.mu.Unlock()
}

func (r *codexAdaptiveRouter) accountSnapshot(authID string) codexAdaptiveAccount {
	if r == nil {
		return codexAdaptiveAccount{limit: codexAdaptiveDefaultConcurrency}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	state := *r.accountLocked(authID)
	return state
}

func (r *codexAdaptiveRouter) release(leaseID string) {
	if r == nil || leaseID == "" {
		return
	}
	r.mu.Lock()
	lease, ok := r.leases[leaseID]
	if ok {
		state := r.accountLocked(lease.authID)
		if state.inFlight > 0 {
			state.inFlight--
		}
		delete(r.leases, leaseID)
		close(lease.done)
		r.notifyLocked()
	}
	r.mu.Unlock()
}

func (r *codexAdaptiveRouter) releaseOptions(options cliproxyexecutor.Options) {
	r.release(adaptiveLeaseID(options))
}

func (r *codexAdaptiveRouter) observe(result Result) {
	if r == nil || result.AuthID == "" || !strings.EqualFold(strings.TrimSpace(result.Provider), "codex") {
		return
	}
	leaseID := adaptiveLeaseID(result.Options)
	if leaseID == "" {
		return
	}
	r.mu.Lock()
	lease, ok := r.leases[leaseID]
	if !ok || lease.authID != result.AuthID {
		r.mu.Unlock()
		return
	}
	state := r.accountLocked(result.AuthID)
	if state.inFlight > 0 {
		state.inFlight--
	}
	delete(r.leases, leaseID)
	close(lease.done)
	status := 0
	if result.Error != nil {
		status = result.Error.HTTPStatus
	}
	if status == http.StatusTooManyRequests {
		state.limit = maxInt(1, state.limit/2)
		state.successes = 0
	} else if result.Success {
		state.successes++
		if state.successes >= codexAdaptiveSuccessesBeforeIncrease && state.limit < codexAdaptiveMaxConcurrency {
			state.limit++
			state.successes = 0
		}
	}
	r.notifyLocked()
	r.mu.Unlock()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

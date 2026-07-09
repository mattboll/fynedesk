package compositor

/*
#cgo pkg-config: wlroots
#cgo CFLAGS: -DWLR_USE_UNSTABLE
#include <wlr/types/wlr_output.h>

// output_cycle_reset recovers an output whose page-flip completion event was
// lost (seen on i915 after suspend/resume): wlroots' pending_page_flip stays
// armed forever and every nonblocking buffer commit fails with "a page-flip
// is already pending" (backend/drm/drm.c), freezing the output while the
// rest of the compositor keeps running.
//
// The recovery is a full off/on cycle, mirroring what wlroots itself does to
// restore outputs after a VT switch (backend/drm/backend.c,
// handle_session_active: "first disable all CRTCs, then light up the ones we
// were using before"). States built with wlr_output_state_set_enabled() and
// set_mode() carry allow_reconfiguration=true, so the DRM backend issues
// BLOCKING modeset commits that bypass the nonblock-only pending-flip guard:
//
//   - the off commit needs no framebuffer, succeeds with the flip still
//     marked pending, and clears pending_page_flip (drm_crtc_commit calls
//     drm_connector_set_pending_page_flip on every success);
//   - the on commit changes ENABLED, so output_ensure_buffer attaches an
//     empty buffer and the full modeset lights the CRTC back up.
//
// A single re-enable commit without the off step would NOT work here: with
// ENABLED and MODE unchanged, wlr_output_commit_state strips them as no-ops,
// output_ensure_buffer skips the buffer, and the atomic commit fails on a
// NULL primary framebuffer.
//
// Costs a visible blank flash — acceptable for an output that has already
// been frozen for seconds.
static int output_cycle_reset(struct wlr_output *output) {
	struct wlr_output_mode *mode = output->current_mode;
	struct wlr_output_state state;

	wlr_output_state_init(&state);
	wlr_output_state_set_enabled(&state, false);
	int ok = wlr_output_commit_state(output, &state) ? 1 : 0;
	wlr_output_state_finish(&state);
	if (!ok) {
		return 0;
	}

	// If the enable commit fails the output is left off; the next recovery
	// attempt re-runs the cycle (the off commit is then a no-op success)
	// until the kernel accepts the modeset again.
	wlr_output_state_init(&state);
	wlr_output_state_set_enabled(&state, true);
	if (mode != NULL) {
		wlr_output_state_set_mode(&state, mode);
	}
	ok = wlr_output_commit_state(output, &state) ? 1 : 0;
	wlr_output_state_finish(&state);
	return ok;
}
*/
import "C"

import (
	"log"
	"time"
)

// Thresholds for declaring an output's page-flip chain stuck. The C frame
// watchdog (output.go) keeps renderOutput running at ~30 FPS while the chain
// is down, so a genuine stall accumulates failures fast. Transient EBUSY
// misses (a flip legitimately in flight when a frame fires) reset on the next
// successful commit and never build a streak beyond 2-3.
const (
	stallMinFailures    = 30
	stallMinDuration    = 2 * time.Second
	recoveryCooldown    = 3 * time.Second
	recoveryCooldownMax = 30 * time.Second
)

// noteCommitFailure tracks a failed scene commit on this output and, once
// the failure streak qualifies as a stall, forces an off/on modeset cycle to
// clear the stuck page flip. Called from renderOutput on the main thread.
func (s *server) noteCommitFailure(out *outputState) {
	now := time.Now()
	if out.commitFails == 0 {
		out.commitFailSince = now
	}
	out.commitFails++

	if s.nestedMode {
		return // nested backend has no DRM chain to reset
	}
	if out.commitFails < stallMinFailures || now.Sub(out.commitFailSince) < stallMinDuration {
		return
	}

	// Back off between attempts so a genuinely unrecoverable output (cable
	// pulled mid-stall, kernel refusing modesets) is retried at a gentle
	// pace instead of being hammered with modesets.
	cooldown := time.Duration(out.recoveryTries+1) * recoveryCooldown
	if cooldown > recoveryCooldownMax {
		cooldown = recoveryCooldownMax
	}
	if !out.lastRecovery.IsZero() && now.Sub(out.lastRecovery) < cooldown {
		return
	}
	out.lastRecovery = now
	out.recoveryTries++

	stalled := now.Sub(out.commitFailSince).Round(100 * time.Millisecond)
	log.Printf("[RECOVERY] %s: %d failed commits over %v — forcing off/on modeset cycle (attempt %d)\n",
		out.output.Name(), out.commitFails, stalled, out.recoveryTries)

	if C.output_cycle_reset(outputPtr(out.output)) == 0 {
		log.Printf("[RECOVERY] %s: modeset cycle failed, will retry\n", out.output.Name())
		return
	}

	// The cycle queued a fresh page flip; schedule a frame so the scene
	// repaints the empty modeset buffer as soon as its completion fires.
	scheduleOutputFrame(out.output)
}

// noteCommitSuccess resets the failure streak. Called from renderOutput on
// the main thread after every successful scene commit.
func (s *server) noteCommitSuccess(out *outputState) {
	if out.commitFails == 0 {
		return
	}
	if out.recoveryTries > 0 || out.commitFails >= stallMinFailures {
		log.Printf("[RECOVERY] %s: commit chain recovered after %d failures (%v, %d recovery attempts)\n",
			out.output.Name(), out.commitFails,
			time.Since(out.commitFailSince).Round(100*time.Millisecond), out.recoveryTries)
	}
	out.commitFails = 0
	out.recoveryTries = 0
}

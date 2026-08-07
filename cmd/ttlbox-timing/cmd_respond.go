// Copyright (2026) Christophe Pallier <christophe@pallier.org>
// Co-authored by Claude Opus 5.
// Licensed under the Apache License, Version 2.0 (see LICENSE.txt).

package main

import (
	"fmt"
	"time"

	ttlbox "github.com/neurospin/neurospin-meg-ttl-box"
	"github.com/spf13/cobra"
)

var (
	respTrials   int
	respRTMs     int
	respTrigLine uint8
	respSelfBit  uint8
	respRespBit  uint8
	respWidthMs  int
	respISIMs    int
	respPollMs   int
	respLegacy   bool
)

var respondCmd = &cobra.Command{
	Use:   "respond",
	Short: "Input path, notification latency and closed loop (blocks B5-B7)",
	Long: `Trigger a BBTKv3 running a Digital Stimulus Response Echo program and
measure every stage of the loop back to the host.

The BBTK must already be programmed and running, in another terminal:

    bbtk-trigger-response -any -i TTLin1 -o TTLout1 -rt <R> -d 20

-any is not optional. The default pattern match demands an exact match of the
whole input port, every other line low, and simply never fires otherwise — with
no error to tell you so.

Each trial pulses the trigger line, which reaches both the BBTK's TTLin1 and the
box's own input, so the firmware timestamps its own edge. R milliseconds later
the BBTK answers on TTLout1 into a second box input, which the firmware
timestamps too. That yields four instants per trial: the host's write, the
box's own edge, the answer's edge, and the moment the host learned of it.

  host write -> own edge     the write latency, measured again
  own edge   -> answer edge  the BBTK's fixed delay plus the box's detection
  answer edge -> host learns the notification latency, what polling costs
  host write -> host learns  the closed loop, the number a real paradigm sees

Sweeping R and regressing the second interval on it is the point of the block.
The slope is the box's clock rate against the BBTK's, over a lever arm of 500x;
the intercept is the fixed latency of the pair, which is the only external check
that the box's micros() timestamps are metrically right and not merely precise;
and the residual spread is an upper bound on how long the firmware can take to
notice an input, since nothing else in the chain varies.

--legacy reruns the same physical stimuli through the pre-v1 button-polling
path, which has no device timestamps at all. Comparing the two is what turns
"timestamps are better" into a number.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRespond()
	},
}

func init() {
	f := respondCmd.Flags()
	f.IntVar(&respTrials, "trials", 100, "number of trials")
	f.IntVar(&respRTMs, "rt", 100, "the delay programmed into the BBTK, in ms (recorded, not set)")
	f.Uint8Var(&respTrigLine, "trigger-line", 1, "output line that triggers the BBTK (0-7)")
	f.Uint8Var(&respSelfBit, "self-input", 1, "input line the trigger loops back to (0-7)")
	f.Uint8Var(&respRespBit, "response-input", 2, "input line the BBTK answers on (0-7)")
	f.IntVar(&respWidthMs, "width", 10, "trigger pulse width in ms")
	f.IntVar(&respISIMs, "isi", 0, "ms between trials (0 = rt + 400)")
	f.IntVar(&respPollMs, "poll", 0, "ms between event polls (0 = poll as fast as the link allows)")
	f.BoolVar(&respLegacy, "legacy", false,
		"use the pre-v1 button-poll path instead of timestamped events")
}

func respISI() time.Duration {
	if respISIMs > 0 {
		return time.Duration(respISIMs) * time.Millisecond
	}
	return time.Duration(respRTMs+400) * time.Millisecond
}

func respondPlan() blockPlan {
	path := "timestamped events"
	if respLegacy {
		path = "legacy button polling"
	}
	d := respISI() * time.Duration(respTrials)
	return blockPlan{
		Name: "respond",
		Steps: []string{
			fmt.Sprintf("%d trials, BBTK delay %d ms, ISI %s, %s",
				respTrials, respRTMs, respISI(), path),
			fmt.Sprintf("trigger line %d -> BBTK TTLin1 and input %d; BBTK TTLout1 -> input %d",
				respTrigLine, respSelfBit, respRespBit),
			fmt.Sprintf("requires: bbtk-trigger-response -any -i TTLin1 -o TTLout1 -rt %d -d 20",
				respRTMs),
		},
		Duration: d + 2*time.Second,
	}
}

func runRespond() error {
	if respSelfBit == respRespBit {
		return fmt.Errorf("--self-input and --response-input are both %d; they must differ",
			respSelfBit)
	}
	if respISI() <= time.Duration(respRTMs+respWidthMs)*time.Millisecond {
		return fmt.Errorf("--isi %s is not longer than rt + width (%d ms); trials would overlap",
			respISI(), respRTMs+respWidthMs)
	}
	if preflight(respondPlan()) {
		return nil
	}
	if respLegacy {
		return runRespondLegacy()
	}

	box, _, err := openBox(ttlbox.CapTimestamps)
	if err != nil {
		return err
	}
	defer box.Close()

	rec, err := newRecorder("respond",
		"rt_ms", "poll_ms", "trial", "host_write_us", "host_recv_us",
		"dev_self_us", "dev_resp_us", "n_events", "answered", "overflow")
	if err != nil {
		return err
	}
	defer rec.Close()

	origin := time.Now()
	clk, err := newClockTracker(box, "respond", origin)
	if err != nil {
		return err
	}
	defer clk.Close()
	if err := clk.Sample(20); err != nil {
		return err
	}

	if err := box.SetTriggerDuration(time.Duration(respWidthMs) * time.Millisecond); err != nil {
		return err
	}
	isi := respISI()
	poll := time.Duration(respPollMs) * time.Millisecond
	timeout := time.Duration(respRTMs)*time.Millisecond + 300*time.Millisecond

	var write, external, notify, loop []time.Duration
	answered := 0

	fmt.Printf("  %d trials, BBTK delay %d ms\n", respTrials, respRTMs)
	for i := range respTrials {
		next := time.Now().Add(isi)
		if err := box.ClearEvents(); err != nil {
			return err
		}
		// ClearEvents re-seeds the firmware's change detector to whatever the
		// lines read now, so the host needs that same baseline to tell which
		// line a later mask differs on.
		baseline, err := box.PollButtons()
		if err != nil {
			return err
		}

		hostWrite := time.Now()
		if err := box.SendTriggerMask(1 << respTrigLine); err != nil {
			return err
		}
		evs, overflow, err := collectUntilBit(box, respRespBit, baseline, poll, timeout)
		if err != nil {
			return err
		}

		self, gotSelf := firstChangeOn(evs, respSelfBit, baseline)
		resp, gotResp := firstChangeOn(evs, respRespBit, baseline)

		var devSelf, devResp ttlbox.DeviceTime
		var recv time.Duration
		if gotSelf {
			devSelf = clk.Unwrap(self.Event.Micros)
		}
		if gotResp {
			devResp = clk.Unwrap(resp.Event.Micros)
			recv = resp.Host.Sub(origin)
			answered++
		}
		rec.Row(respRTMs, respPollMs, i, hostWrite.Sub(origin), recv,
			devSelf, devResp, len(evs), gotResp, overflow)

		if gotSelf {
			write = append(write, clk.HostTime(devSelf).Sub(hostWrite))
		}
		if gotSelf && gotResp {
			external = append(external, time.Duration(devResp-devSelf)*time.Microsecond)
		}
		if gotResp {
			notify = append(notify, resp.Host.Sub(clk.HostTime(devResp)))
			loop = append(loop, resp.Host.Sub(hostWrite))
		}
		sleepUntil(next)
	}
	if err := clk.Sample(20); err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("  host write -> own edge     %v\n", summarise(write))
	fmt.Printf("  own edge -> answer edge    %v\n", summarise(external))
	fmt.Printf("  answer edge -> host learns %v\n", summarise(notify))
	fmt.Printf("  closed loop                %v\n", summarise(loop))
	reportUnanswered(answered, respTrials)
	fmt.Printf("  wrote %s\n", rec.Path)
	return nil
}

// runRespondLegacy repeats the block through the pre-v1 path, where the only
// timestamp is the host's own and the resolution is the poll interval.
func runRespondLegacy() error {
	box, _, err := openBox(0)
	if err != nil {
		return err
	}
	defer box.Close()

	rec, err := newRecorder("respond-legacy",
		"rt_ms", "poll_ms", "trial", "host_write_us", "host_recv_us", "answered")
	if err != nil {
		return err
	}
	defer rec.Close()

	origin := time.Now()
	if err := box.SetTriggerDuration(time.Duration(respWidthMs) * time.Millisecond); err != nil {
		return err
	}
	isi := respISI()
	poll := time.Duration(respPollMs) * time.Millisecond
	timeout := time.Duration(respRTMs)*time.Millisecond + 300*time.Millisecond
	respMask := uint8(1) << respRespBit

	var loop []time.Duration
	answered := 0

	fmt.Printf("  %d trials through the legacy button-poll path, BBTK delay %d ms\n",
		respTrials, respRTMs)
	for i := range respTrials {
		next := time.Now().Add(isi)
		baseline, err := box.PollButtons()
		if err != nil {
			return err
		}

		hostWrite := time.Now()
		if err := box.SendTriggerMask(1 << respTrigLine); err != nil {
			return err
		}

		// Watch only the response line: the trigger's own loopback edge is on
		// another bit and would otherwise be mistaken for the answer.
		var recv time.Duration
		got := false
		deadline := time.Now().Add(timeout)
		last := baseline
		for time.Now().Before(deadline) {
			mask, err := box.PollButtons()
			now := time.Now()
			if err != nil {
				return err
			}
			if (mask^last)&respMask != 0 {
				recv, got = now.Sub(origin), true
				loop = append(loop, now.Sub(hostWrite))
				answered++
				break
			}
			last = mask
			if poll > 0 {
				time.Sleep(poll)
			}
		}
		rec.Row(respRTMs, respPollMs, i, hostWrite.Sub(origin), recv, got)
		sleepUntil(next)
	}

	fmt.Println()
	fmt.Printf("  closed loop (legacy path)  %v\n", summarise(loop))
	fmt.Println("  no device timestamps exist on this path: every figure above is")
	fmt.Println("  floored by the poll interval and the USB round trip, which is")
	fmt.Println("  the whole reason the timestamped path was added.")
	reportUnanswered(answered, respTrials)
	fmt.Printf("  wrote %s\n", rec.Path)
	return nil
}

// reportUnanswered explains a silent BBTK, which has one overwhelmingly likely
// cause and no error message of its own.
func reportUnanswered(answered, trials int) {
	if answered == trials {
		return
	}
	fmt.Printf("\n  %d of %d trials got no answer from the BBTK.\n", trials-answered, trials)
	if answered == 0 {
		fmt.Println("  Nothing answered at all. The usual cause is a DSRE program left on")
		fmt.Println("  the default STYP PATT, which requires an exact match of the whole")
		fmt.Println("  input port and fires for nothing else. Re-run bbtk-trigger-response")
		fmt.Println("  with -any, and check the wiring and the shared ground.")
	}
}

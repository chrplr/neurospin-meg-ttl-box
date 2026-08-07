#!/usr/bin/env python3
"""Analyse the CSVs written by `ttlbox-timing`.

Usage:  ./analyse-timing.py <session-directory> [...]

Each measurement block writes raw per-trial rows and summarises nothing, so all
the arithmetic lives here and can be redone, re-cut or argued with without
spending hardware time again. Stdlib only, deliberately: this has to run on the
stimulus PC, which has no scientific Python stack.
"""
import csv
import glob
import os
import statistics as st
import sys

# ---------------------------------------------------------------- helpers


def read(path):
    with open(path) as f:
        return list(csv.DictReader(f))


def quantile(sorted_vals, q):
    """Nearest-rank quantile: reports a value that was actually observed, which
    is what a claim about a worst case should rest on."""
    if not sorted_vals:
        return float("nan")
    i = int(q * len(sorted_vals) + 0.5) - 1
    return sorted_vals[min(max(i, 0), len(sorted_vals) - 1)]


def describe(vals, unit="ms"):
    if not vals:
        return "  no data"
    s = sorted(vals)
    return (f"  n={len(s)}  min {s[0]:.3f}  p50 {quantile(s, .5):.3f}  "
            f"p95 {quantile(s, .95):.3f}  p99 {quantile(s, .99):.3f}  "
            f"p99.9 {quantile(s, .999):.3f}  max {s[-1]:.3f}  "
            f"mean {st.mean(s):.3f} {unit}")


def linfit(xs, ys):
    """Least squares y = a + b x, centred on the means: raw device microseconds
    reach 4e9 and uncentred sums of squares lose milliseconds of precision."""
    n = len(xs)
    if n < 2:
        return None
    mx, my = st.mean(xs), st.mean(ys)
    sxx = sum((x - mx) ** 2 for x in xs)
    if sxx == 0:
        return None
    sxy = sum((x - mx) * (y - my) for x, y in zip(xs, ys))
    b = sxy / sxx
    a = my - b * mx
    resid = [y - (a + b * x) for x, y in zip(xs, ys)]
    rms = (sum(r * r for r in resid) / n) ** 0.5
    return a, b, rms, max(abs(r) for r in resid)


def clock_fit(session, name):
    """Fit host time against device time from a block's -clock.csv.

    Returns a function converting device µs to host µs (both relative to the
    block's own origin), or None. Rate matters as much as offset: the AVR runs
    on a ceramic resonator, so an offset taken once at the start drifts.
    """
    path = os.path.join(session, f"{name}-clock.csv")
    if not os.path.exists(path):
        return None, None
    rows = read(path)
    xs = [float(r["device_us"]) for r in rows]
    ys = [float(r["host_us"]) for r in rows]
    fit = linfit(xs, ys)
    if fit is None:
        if not rows:
            return None, None
        # One sample: assume equal rates and use its offset.
        off = ys[0] - xs[0]
        return (lambda d: d + off), None
    a, b, rms, worst = fit
    rts = sorted(float(r["roundtrip_us"]) for r in rows if r.get("roundtrip_us"))
    best_rt = rts[0] if rts else float("nan")
    return (lambda d: a + b * d), (len(rows), b, rms, worst, best_rt)


def section(title):
    print(f"\n{'=' * 72}\n{title}\n{'=' * 72}")


def report_clock(info):
    """Report the clock fit, and how far a converted timestamp can be trusted.

    Rate and offset are not equally well determined, and conflating them is how
    a device→host conversion goes quietly wrong. The rate is a slope over the
    whole run and is tight; the offset is an intercept estimated by bracketing a
    get_micros round trip, and Cristian's midpoint is only exact if the two
    directions take equally long. The realistic bound on the offset is therefore
    half the SHORTEST round trip achieved, which for this device has a floor of
    about 2.4 ms — one USB frame each way plus four reply bytes at 115200 on the
    16u2 UART. That floor is not reducible by taking more samples.
    """
    if info is None:
        return
    n, rate, rms, worst, best_rt = info
    print(f"  clock: {n} samples, rate error {(rate - 1) * 1e6:+.2f} ppm, "
          f"residual RMS {rms:.1f} µs, max {worst:.1f} µs")
    if n < 10:
        print(f"  clock: only {n} samples — too few to separate drift from "
              "round-trip noise; raise --clock-every")
    print(f"  clock: best round trip {best_rt / 1000:.3f} ms, so any single "
          f"device→host conversion carries up to ±{best_rt / 2000:.3f} ms of "
          "offset error")


def offset_bound_ms(info):
    """Half the shortest clock round trip, in ms: the error a conversion can carry."""
    if info is None:
        return float("inf")
    return info[4] / 2000


# ---------------------------------------------------------------- blocks


def analyse_widths(session):
    path = os.path.join(session, "widths.csv")
    if not os.path.exists(path):
        return
    section("B1  PULSE WIDTH — realised vs requested")
    print("millis() truncates, so the realised width should be uniform on [w-1, w]:\n"
          "a bias of about -0.5 ms at every width, which cannot be averaged away.")
    rows = read(path)
    print(f"\n{'req ms':>7}{'n':>5}{'min':>9}{'median':>9}{'max':>9}"
          f"{'spread':>9}{'mean err':>10}")
    for req in sorted({float(r["requested_ms"]) for r in rows}):
        w = [float(r["dev_width_us"]) / 1000 for r in rows
             if float(r["requested_ms"]) == req and int(r["n_events"]) == 2]
        if not w:
            print(f"{req:>7.0f}  no complete pulses")
            continue
        print(f"{req:>7.0f}{len(w):>5}{min(w):>9.3f}{st.median(w):>9.3f}"
              f"{max(w):>9.3f}{max(w) - min(w):>9.3f}{st.mean(w) - req:>+10.3f}")
    incomplete = [r for r in rows if int(r["n_events"]) != 2]
    if incomplete:
        print(f"\n  {len(incomplete)} of {len(rows)} pulses did not yield two edges "
              "— check the loopback wiring")
    print("\nMeasured by the box's own micros() through the loopback: 4 µs resolution,\n"
          "against the BBTK's 0.25 ms. Compare the two before quoting either.")


def analyse_codechange(session):
    path = os.path.join(session, "codechange.csv")
    if not os.path.exists(path):
        return
    section("B2  TRIGGER-CODE CHANGE — is an intermediate value visible?")
    rows = read(path)
    arms = {}
    for r in rows:
        arms.setdefault(r["arm"], []).append(r)
    for arm, rs in arms.items():
        two = [r for r in rs if int(r["n_events"]) == 2]
        glitches = [float(r["glitch_us"]) for r in two]
        print(f"\n  {arm}: {len(two)} of {len(rs)} trials showed an intermediate code")
        if glitches:
            print(f"    intermediate lasted: min {min(glitches):.1f}  "
                  f"median {st.median(glitches):.1f}  max {max(glitches):.1f} µs")
            masks = {r["mask1"] for r in two}
            print(f"    intermediate input masks seen: {sorted(masks)}")
    if "legacy" in arms and "atomic" in arms:
        legacy_two = sum(1 for r in arms["legacy"] if int(r["n_events"]) == 2)
        atomic_two = sum(1 for r in arms["atomic"] if int(r["n_events"]) == 2)
        print()
        if legacy_two == 0:
            print("  POSITIVE CONTROL FAILED: the legacy arm produced no intermediate")
            print("  either, so the atomic arm's clean result is not evidence of")
            print("  anything. Do not quote it; re-run the block.")
        elif atomic_two == 0:
            print("  The legacy path exposes a wrong code and the atomic path never")
            print("  does, measured by the same apparatus in the same session. That")
            print("  is what makes opcode 17 worth feature-detecting.")
        else:
            print(f"  The atomic path also glitched, in {atomic_two} trials. That")
            print("  contradicts a single-instruction port write and needs explaining")
            print("  before anything else here is trusted.")


def analyse_latency(session):
    """Every latency run in the session, tagged or not.

    Runs are kept in separate files rather than pooled because they differ in
    the thing being compared — an idle host, a loaded one, a real-time priority
    — and a pooled distribution would hide exactly the difference the block
    exists to show.
    """
    paths = [p for p in sorted(glob.glob(os.path.join(session, "latency*.csv")))
             if "-clock" not in p]
    results = [analyse_one_latency(session, p) for p in paths]
    compare_conditions([r for r in results if r])


def analyse_one_latency(session, path):
    """One latency run.

    Ordered deliberately: the quantities that stay inside a single clock come
    first, and the one that crosses clocks comes last with its error bar. An
    absolute host→device latency is the number everyone wants, but it is the
    least trustworthy thing here — it is around 1.5 ms and the offset estimate
    needed to compute it carries over 1 ms of possible error. The round trip and
    the inter-onset intervals need no offset at all and so are quotable.
    """
    name = os.path.basename(path)[:-4]
    section(f"B3/B9  HOST -> DEVICE LATENCY  ({name})")
    rows = read(path)
    live = [r for r in rows if int(r["n_events"]) > 0]
    if not live:
        print("  no pulse produced an event — check the loopback wiring")
        return None
    cond = live[0].get("condition", "-")

    # 1. Host write -> host learns of the edge. Both timestamps come from the
    #    host clock, so no device clock and no offset estimate is involved. It
    #    bounds the write latency from above and is the safest thing to compare
    #    between conditions.
    trip = [(float(r["host_recv_us"]) - float(r["host_write_us"])) / 1000
            for r in live if float(r["host_recv_us"]) > 0]
    print(f"\n  host write -> host learns of the edge (no device clock involved):")
    print(describe(trip))

    # 2. Interval between successive edges, entirely in device time. Immune to
    #    the offset, and affected by the rate only at the ppm level.
    onsets = [float(r["dev_rise_us"]) for r in live]
    isis = [(b - a) / 1000 for a, b in zip(onsets, onsets[1:])]
    if isis:
        print("\n  inter-onset intervals as the DEVICE saw them:")
        print(describe(isis))

    lost = len(rows) - len(live)
    if lost:
        print(f"\n  {lost} of {len(rows)} pulses produced no event at all")

    # 3. The offset-dependent figure, last and hedged.
    to_host, info = clock_fit(session, name)
    if to_host is None:
        print("\n  no clock samples — absolute latency cannot be computed")
        return {"cond": cond, "trip": trip, "isis": isis, "abs": None, "bound": float("inf")}
    print()
    report_clock(info)
    absolute = [(to_host(float(r["dev_rise_us"])) - float(r["host_write_us"])) / 1000
                for r in live]
    bound = offset_bound_ms(info)
    print(f"\n  absolute host->device latency (crosses clocks — see the bound above):")
    print(describe(absolute))
    if bound > 0.25 * abs(quantile(sorted(absolute), .5) or 1):
        print(f"\n  WARNING: the offset error (±{bound:.3f} ms) is a large fraction of")
        print("  the latency itself, so this figure is an estimate with an error bar")
        print("  comparable to the quantity. Do not quote it as a measurement, and do")
        print("  not compare it between conditions — use the round trip above, or the")
        print("  BBTK's onset-to-onset intervals, which need no offset at all.")

    return {"cond": cond, "trip": trip, "isis": isis,
            "abs": absolute, "bound": bound}


def compare_conditions(results):
    """Compare host conditions, using only the offset-free measures.

    The absolute latency is deliberately not compared. Its offset error is
    independent per run, so a difference between two conditions can be entirely
    an artefact of two different offset estimates — which is exactly what
    produced a 'faster under load' result the first time this block was run.
    """
    if len(results) < 2:
        return
    section("B3/B9  CONDITIONS COMPARED (offset-free measures only)")
    print(f"\n{'condition':<12}{'trip p50':>10}{'trip p99.9':>12}{'trip max':>10}"
          f"{'ISI p99.9':>11}{'ISI max':>10}")
    for r in results:
        t, i = sorted(r["trip"]), sorted(r["isis"])
        print(f"{r['cond']:<12}{quantile(t, .5):>10.3f}{quantile(t, .999):>12.3f}"
              f"{(t[-1] if t else 0):>10.3f}{quantile(i, .999):>11.3f}"
              f"{(i[-1] if i else 0):>10.3f}")
    print("\n  All figures in ms, and all of them stay inside one clock.")

    worst = max(results, key=lambda r: quantile(sorted(r["trip"]), .999))
    best = min(results, key=lambda r: quantile(sorted(r["trip"]), .999))
    if worst["cond"] != best["cond"]:
        print(f"\n  Worst tail: {worst['cond']!r}. What matters for an experiment is not")
        print("  the median but the trials in the tail, since those are the ones that")
        print("  land a trigger in the wrong place and cannot be recovered afterwards.")

    if any(r["abs"] for r in results):
        print("\n  Absolute latency is NOT compared here. Each run estimates the")
        print("  host/device offset independently, and that estimate carries more")
        print("  error than the difference between conditions, so a comparison would")
        print("  mostly reflect the two offsets rather than the two conditions.")


def analyse_drift(session):
    path = os.path.join(session, "drift.csv")
    if not os.path.exists(path):
        return
    section("B4  CLOCK DRIFT — device against host")
    rows = [r for r in read(path) if int(r["n_events"]) > 0]
    if len(rows) < 2:
        print("  not enough pulses")
        return
    xs = [float(r["dev_rise_us"]) for r in rows]
    ys = [float(r["host_write_us"]) for r in rows]
    span_h = (xs[-1] - xs[0]) / 1e6 / 3600
    fit = linfit(xs, ys)
    if fit is None:
        print("  degenerate fit")
        return
    _, b, rms, worst = fit
    ppm = (b - 1) * 1e6
    print(f"  {len(rows)} pulses over {span_h:.2f} h")
    print(f"  device rate error vs host: {ppm:+.2f} ppm")
    print(f"  fit residuals: RMS {rms:.0f} µs, max {worst:.0f} µs")
    print(f"  => an offset taken once drifts {abs(ppm) * 3.6:.1f} ms per hour")
    _, info = clock_fit(session, "drift")
    report_clock(info)
    print("\n  This compares two clocks and cannot say which is wrong. If a BBTK")
    print("  captured the same pulses, fit its onsets against both before")
    print("  attributing the error to the AVR's ceramic resonator.")
    print("  The residuals, not the ppm, are what bounds a single conversion.")


def respond_files(session, kind):
    """Files for one arm of the respond block.

    The delay sweep and the poll sweep both write respond-*.csv but answer
    different questions, and pooling them would put five times the weight at the
    one delay the poll sweep uses — which is precisely the point the regression
    slope leans on. They are kept apart by filename.
    """
    pat = {"rt": "respond-rt*.csv",
           "poll": "respond-poll*.csv",
           "legacy": "respond-legacy*.csv"}[kind]
    paths = glob.glob(os.path.join(session, pat))
    if kind == "rt" and not paths:
        # A single untagged run, from `ttlbox-timing respond` on its own.
        paths = glob.glob(os.path.join(session, "respond.csv"))
    # Sort by the number in the tag, not lexically: a plain sort puts rt500
    # between rt50 and rt10, which makes a swept table unreadable.
    def key(p):
        digits = "".join(c for c in os.path.basename(p) if c.isdigit())
        return (int(digits) if digits else -1, p)
    return sorted((p for p in paths if "-clock" not in p), key=key)


def respond_row(session, path):
    """One line of the respond table, plus the raw intervals for the fit."""
    rows = [r for r in read(path) if r["answered"] == "true"]
    if not rows:
        print(f"  {os.path.basename(path)}: no trial was answered")
        return None
    to_host, _ = clock_fit(session, os.path.basename(path)[:-4])
    rt = float(rows[0]["rt_ms"])
    poll = rows[0]["poll_ms"]
    ext = [(float(r["dev_resp_us"]) - float(r["dev_self_us"])) / 1000 for r in rows]
    notify, loop = [], []
    if to_host:
        notify = sorted((float(r["host_recv_us"]) - to_host(float(r["dev_resp_us"]))) / 1000
                        for r in rows)
        loop = sorted((float(r["host_recv_us"]) - float(r["host_write_us"])) / 1000
                      for r in rows)
    sd = st.stdev(ext) * 1000 if len(ext) > 1 else float("nan")
    print(f"{rt:>7.0f}{poll:>7}{len(ext):>5}{st.median(ext):>15.3f}{sd:>10.1f}"
          f"{quantile(notify, .5) if notify else float('nan'):>15.3f}"
          f"{quantile(loop, .5) if loop else float('nan'):>13.3f}")
    return rt, ext


def analyse_respond(session):
    rt_paths = respond_files(session, "rt")
    poll_paths = respond_files(session, "poll")
    if not rt_paths and not poll_paths:
        return
    section("B5/B6/B7  INPUT PATH, NOTIFICATION AND CLOSED LOOP")

    all_rt, all_ext = [], []
    print(f"\n{'rt ms':>7}{'poll':>7}{'n':>5}{'median ext ms':>15}{'sd µs':>10}"
          f"{'notify p50 ms':>15}{'loop p50 ms':>13}")
    print("  -- delay sweep --")
    for p in rt_paths:
        got = respond_row(session, p)
        if got:
            rt, ext = got
            all_rt += [rt] * len(ext)
            all_ext += ext
    if poll_paths:
        # Excluded from the regression on purpose: every one of these sits at
        # the same delay, and pooling them would weight that point five-fold.
        print("  -- poll sweep (one delay, excluded from the fit) --")
        for p in poll_paths:
            respond_row(session, p)

    fit = linfit(all_rt, all_ext)
    if fit and len({r for r in all_rt}) > 1:
        a, b, rms, worst = fit
        print(f"\n  regression of measured interval on programmed delay, "
              f"n={len(all_rt)}:")
        print(f"    slope     {b:.6f}   ({(b - 1) * 1e6:+.0f} ppm from unity)")
        print(f"    intercept {a:+.3f} ms")
        print(f"    residuals RMS {rms * 1000:.1f} µs, max {worst * 1000:.1f} µs")
        print("\n  The slope is the box's clock rate against the BBTK's over the whole")
        print("  sweep — the only external check that its micros() timestamps are")
        print("  metrically right and not merely precise. The intercept is the fixed")
        print("  latency of the pair: the BBTK's own trigger-to-output delay plus the")
        print("  box's input detection. The residuals bound how long the firmware can")
        print("  take to notice an input, since nothing else in the chain varies.")
    else:
        print("\n  only one delay was run — sweep --rt to get the slope and intercept")

    legacy = respond_files(session, "legacy")
    if legacy:
        print("\n  legacy button-poll path, same physical stimuli:")
        for p in legacy:
            rows = [r for r in read(p) if r["answered"] == "true"]
            if not rows:
                continue
            loop = [(float(r["host_recv_us"]) - float(r["host_write_us"])) / 1000
                    for r in rows]
            print(f"    poll {rows[0]['poll_ms']} ms, closed loop:")
            print("  " + describe(loop))
        print("\n  This path has no device timestamps at all, so its resolution is the")
        print("  poll interval plus a USB round trip however long the run. The gap")
        print("  between it and the timestamped path is what timestamping buys.")


def analyse_splitwrite(session):
    path = os.path.join(session, "splitwrite.csv")
    if not os.path.exists(path):
        return
    section("B8  A COMMAND SPLIT ACROSS TWO WRITES")
    rows = read(path)
    arms = {}
    for r in rows:
        if int(r["n_events"]) == 2:
            arms.setdefault(r["arm"], []).append(float(r["dev_width_us"]) / 1000)
    for arm in ("control", "split"):
        if arm in arms:
            print(f"\n  {arm} realised width (ms):")
            print(describe(arms[arm]))
    if "control" in arms and "split" in arms:
        r0 = rows[0]
        predicted = max(float(r0["stall_start_ms"]) + float(r0["stall_ms"])
                        - float(r0["requested_ms"]), 0)
        observed = st.median(arms["split"]) - st.median(arms["control"])
        print(f"\n  median extension {observed:+.3f} ms, predicted {predicted:.3f} ms")
        if predicted > 0 and observed > predicted / 2:
            print("\n  The firmware's argument read is an unbounded spin, and nothing")
            print("  else runs during it — not the input sampling, not the pulse")
            print("  teardown. Client contract: build the whole command and write it")
            print("  once. The typed methods in this package already do.")
        elif predicted > 0:
            print("\n  No extension of the predicted size. Check that the two writes")
            print("  really left the host as separate USB frames before concluding")
            print("  the firmware is unaffected.")


def analyse_overflow(session):
    path = os.path.join(session, "overflow.csv")
    if not os.path.exists(path):
        return
    section("B11  SUSTAINED LOAD AND QUEUE OVERFLOW")
    rows = read(path)
    for phase in ("drain", "stall"):
        rs = [r for r in rows if r["phase"] == phase]
        if not rs:
            continue
        pulses = sum(int(r["pulses"]) for r in rs)
        events = sum(int(r["events"]) for r in rs)
        flagged = sum(1 for r in rs if r["overflow"] == "true")
        print(f"\n  {phase}: {len(rs)} windows, {pulses} pulses, {events} events "
              f"({2 * pulses} would be two per pulse)")
        print(f"    {flagged} windows reported an overflow")
    stall = [r for r in rows if r["phase"] == "stall"]
    if stall and not any(r["overflow"] == "true" for r in stall):
        print("\n  No overflow was reported despite the deliberate stalls. Either the")
        print("  stalls were too short to fill 32 slots at this rate, or the flag is")
        print("  not reaching the host. Do not read this as 'the queue never")
        print("  overflows' without settling which.")
    elif stall:
        print("\n  Overflow is reported, not hidden. An affected trial lost")
        print("  transitions rather than delaying them, so it is wrong, not late,")
        print("  and belongs in the discard pile.")


def main():
    if len(sys.argv) < 2:
        sys.exit(__doc__)
    for session in sys.argv[1:]:
        print(f"\n{'#' * 72}\n# {session}\n{'#' * 72}")
        analyse_widths(session)
        analyse_codechange(session)
        analyse_latency(session)
        analyse_drift(session)
        analyse_respond(session)
        analyse_splitwrite(session)
        analyse_overflow(session)
        print()


if __name__ == "__main__":
    main()

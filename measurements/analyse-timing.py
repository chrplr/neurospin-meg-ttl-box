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
    return (lambda d: a + b * d), (len(rows), b, rms, worst)


def section(title):
    print(f"\n{'=' * 72}\n{title}\n{'=' * 72}")


def report_clock(info):
    if info is None:
        return
    n, rate, rms, worst = info
    print(f"  clock: {n} samples, rate error {(rate - 1) * 1e6:+.2f} ppm, "
          f"residual RMS {rms:.1f} µs, max {worst:.1f} µs")


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
    for path in paths:
        analyse_one_latency(session, path)


def analyse_one_latency(session, path):
    name = os.path.basename(path)[:-4]
    section(f"B3/B9  HOST -> DEVICE LATENCY  ({name})")
    rows = read(path)
    to_host, info = clock_fit(session, name)
    if to_host is None:
        print("  no clock samples — cannot convert device time to host time")
        return
    report_clock(info)
    by_cond = {}
    for r in rows:
        if int(r["n_events"]) == 0:
            continue
        lat = (to_host(float(r["dev_rise_us"])) - float(r["host_write_us"])) / 1000
        by_cond.setdefault(r.get("condition", "-"), []).append(lat)
    for cond, vals in by_cond.items():
        print(f"\n  condition {cond!r}:")
        print(describe(vals))
    lost = sum(1 for r in rows if int(r["n_events"]) == 0)
    if lost:
        print(f"\n  {lost} of {len(rows)} pulses produced no event at all")
    print("\n  A serial write returns when the kernel takes the bytes, not when they")
    print("  reach the wire, so this is an upper bound on the host's contribution.")
    print("  The BBTK arm of the same session bounds it from outside.")

    onsets = [float(r["dev_rise_us"]) for r in rows if int(r["n_events"]) > 0]
    if len(onsets) > 2:
        isis = [(b - a) / 1000 for a, b in zip(onsets, onsets[1:])]
        print("\n  inter-onset intervals as the DEVICE saw them (ms):")
        print(describe(isis))


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

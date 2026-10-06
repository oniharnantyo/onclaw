#!/usr/bin/env python3
"""Wrap a suite-v3 scoreboard as a committed baseline with noise-derived gates.

Gates: max_regression = ceil(2*sqrt(p(1-p)/trials), 3 decimals) at the
baseline's own per-lane rate (the README's calibration rule), min_improvement 0.
Usage: wrap3.py <scoreboard.json> <out-baseline.json>
"""
import json, math, sys

sb = json.load(open(sys.argv[1]))
lanes = {}
for t in sb["tasks"]:
    if t["status"] == "infra-error":
        continue
    lanes.setdefault(t["lane"], [0, 0])
    lanes[t["lane"]][1] += 1
    lanes[t["lane"]][0] += 1 if t["status"] == "pass" else 0

def floor(passes, trials):
    p = passes / trials
    f = math.ceil(2 * math.sqrt(p * (1 - p) / trials) * 1000) / 1000
    if f == 0.0:
        f = 0.1  # pragmatic floor at p=1.0 (baseline-1 convention): pure 2σ is 0 there
    return f

gates = {}
tot_p = tot_n = 0
for lane, (p, n) in sorted(lanes.items()):
    gates[lane] = {"max_regression": floor(p, n), "min_improvement": 0.0}
    tot_p += p
    tot_n += n
gates["overall"] = {"max_regression": floor(tot_p, tot_n), "min_improvement": 0.0}

wrap = {
    "schema_version": 1,
    "suite_version": sb["suite_version"],
    "onclaw_git_sha": sb["onclaw_git_sha"],
    "model_pin": sb["model_pin"],
    "gates": gates,
    "scoreboard": sb,
}
json.dump(wrap, open(sys.argv[2], "w"), indent=2)
print(f"lanes: " + ", ".join(f"{l} {p}/{n} ({p/n:.1%}) floor {gates[l]['max_regression']}" for l, (p, n) in sorted(lanes.items())))
print(f"overall {tot_p}/{tot_n} ({tot_p/tot_n:.1%}) floor {gates['overall']['max_regression']}")
print(f"wrote {sys.argv[2]}")

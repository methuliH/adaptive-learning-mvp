"""Fit BKT parameters per skill with pyBKT.

Usage:
    python tools/fit_pybkt.py answers.csv            # readable table
    python tools/fit_pybkt.py answers.csv --json     # JSON for the Go server

Input is the CSV from GET /api/v1/practice/export.csv or `go run ./cmd/simulate`:
order_id,user_id,skill_name,correct[,subject]

The demo server runs this script itself (POST /api/v1/model/fit). Install the
pinned versions in tools/requirements.txt first; see the README for why.
"""
import argparse
import json
import sys

import pandas as pd
import pyBKT
from pyBKT.models import Model

SEP = " :: "  # joins subject and skill so equal unit names in two subjects stay apart


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("csv")
    ap.add_argument("--json", action="store_true", help="print JSON instead of a table")
    ap.add_argument("--min-answers", type=int, default=50, help="skip skills with fewer answers")
    ap.add_argument("--num-fits", type=int, default=3, help="EM restarts per skill; best is kept")
    ap.add_argument("--seed", type=int, default=42)
    args = ap.parse_args()

    df = pd.read_csv(args.csv, dtype={"user_id": str, "skill_name": str, "subject": str})
    if "subject" not in df.columns:
        df["subject"] = ""
    df["skill_name"] = df["subject"].fillna("") + SEP + df["skill_name"]

    counts = df.groupby("skill_name").agg(answers=("correct", "size"), students=("user_id", "nunique"))
    keep = counts[counts["answers"] >= args.min_answers].index
    skipped = [
        {**split(k), "answers": int(r.answers), "reason": f"fewer than {args.min_answers} answers"}
        for k, r in counts.iterrows() if k not in keep
    ]

    params = []
    if len(keep):
        # parallel=False: pyBKT's multiprocessing hangs on Windows without its C++ extension
        model = Model(seed=args.seed, num_fits=args.num_fits, parallel=False)
        model.fit(data=df[df["skill_name"].isin(keep)][["order_id", "user_id", "skill_name", "correct"]])
        fitted = model.params()["value"]
        for k in keep:
            params.append({
                **split(k),
                "prior": round(float(fitted[(k, "prior", "default")]), 4),
                "learn": round(float(fitted[(k, "learns", "default")]), 4),
                "guess": round(float(fitted[(k, "guesses", "default")]), 4),
                "slip": round(float(fitted[(k, "slips", "default")]), 4),
                "answers": int(counts.loc[k, "answers"]),
                "students": int(counts.loc[k, "students"]),
            })

    out = {"pybkt": pyBKT.__version__ if hasattr(pyBKT, "__version__") else "", "rows": len(df),
           "params": params, "skipped": skipped}
    if args.json:
        json.dump(out, sys.stdout)
        return
    print(f"{len(df)} answers")
    for p in params:
        print(f"{p['subject']}/{p['skill']}: prior {p['prior']:.2f}  learn {p['learn']:.2f}  "
              f"guess {p['guess']:.2f}  slip {p['slip']:.2f}  ({p['answers']} answers, {p['students']} students)")
    for s in skipped:
        print(f"skipped {s['subject']}/{s['skill']}: {s['reason']}")


def split(key):
    subject, _, skill = key.partition(SEP)
    return {"subject": subject, "skill": skill}


if __name__ == "__main__":
    main()

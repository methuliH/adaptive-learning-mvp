# LearnSteer Adaptive Practice MVP

A standalone Go service that demonstrates adaptive quizzing for LearnSteer 2.0. It covers Feature 1 (question difficulty) and Feature 2 (next-question prediction with Bayesian Knowledge Tracing) from the R&D plan.

Students answer one question at a time. After each answer the engine updates the student's mastery of that unit and serves the next question at the matching difficulty: easy below 40%, medium from 40%, hard from 70%. At 95% the unit counts as mastered and the student moves on to their weakest unit. pyBKT fits the model's parameters from the answer history.

## Status

| Part | State |
| --- | --- |
| Adaptive engine, practice flow, REST API | Working, unit tested |
| pyBKT fitting (fit, validate, load) | Working, tested end to end against real pyBKT |
| Simulator and demo page | Working |
| Storage | In memory only; restarting clears progress |
| Auth | `X-User-ID` header; no admin check on `/model` routes |
| Sample questions | 45 A/L Chemistry questions, not yet reviewed by the content team |

Not yet integrated into `learnsteer-backend`, the CMS or the React frontend; see [Plugging into learnsteer-backend](#plugging-into-learnsteer-backend).

## Getting started

Requires Go 1.24+ and, for pyBKT fitting, Python 3.

```bash
go run ./cmd/demo
```

Open http://localhost:8090. Practice works straight away; the pyBKT button needs the Python setup below.

### Enable pyBKT fitting

Create a project-local virtual environment once. The demo finds `.venv` automatically.

```bash
python -m venv .venv
```

```bash
.venv/Scripts/python -m pip install -r tools/requirements.txt
```

On macOS or Linux use `.venv/bin/python` instead.

### Tests and simulator

```bash
go test ./...
```

`go test ./fit` includes a real pyBKT fit when `.venv` exists (about 30 seconds); `go test -short ./...` skips it.

```bash
go run ./cmd/simulate
```

The simulator runs 200 fake students, prints how well the engine tracked them, and writes `sim_answers.csv`. Fit it by hand with `.venv/Scripts/python tools/fit_pybkt.py sim_answers.csv`.

## How the next question is chosen

1. Unit: stay in the current unit until it is mastered or has nothing suitable; otherwise the weakest unmastered unit.
2. Difficulty: from the student's mastery of that unit.
3. An unseen question at that difficulty, or failing that at an easier one. **Never a harder one**: a struggling student who runs out of easy questions is not pushed to medium.
4. Otherwise a question they got wrong, once at least 3 other answers have come in between (shown as "Second try").
5. Otherwise move to another unit, with a message saying why.

## pyBKT in the demo

The model panel under the question shows the four BKT numbers each unit uses and where they came from (`default`, `manual` or `fitted`).

1. **Add 150 simulated students** answers questions through the real engine. Each unit has different hidden "true" values, shown in grey under the unit.
2. **Fit with pyBKT** exports every answer, runs `tools/fit_pybkt.py` (pyBKT EM fit per unit), validates the results and loads them. Units with fewer than 50 answers keep their current values. It takes 15 to 60 seconds.
3. From then on the engine uses the fitted values: a new student's starting mastery is the unit's fitted prior, and every update uses the fitted learn, guess and slip.

On 2,733 simulated answers the fit took 18 seconds and landed close to the hidden values:

| Unit | Prior (fitted / true) | Learn | Guess | Slip |
| --- | --- | --- | --- | --- |
| Atomic Structure | 0.22 / 0.25 | 0.14 / 0.15 | 0.18 / 0.20 | 0.08 / 0.08 |
| Chemical Bonding | 0.13 / 0.10 | 0.11 / 0.10 | 0.24 / 0.25 | 0.08 / 0.10 |
| Gaseous State | 0.44 / 0.40 | 0.21 / 0.20 | 0.27 / 0.20 | 0.01 / 0.05 |

**Version pins.** pyBKT 1.4.3 runs here without its C++ extension, which breaks on current libraries: scikit-learn 1.8+ crashes `import pyBKT`, and NumPy 2.4+ crashes fitting. `tools/requirements.txt` pins scikit-learn 1.7.2 and NumPy 2.3.5, and the script fits with `parallel=False` because pyBKT's multiprocessing stalls on Windows.

## Layout

| Path | What it is | Moves into the backend? |
| --- | --- | --- |
| `engine/` | BKT update, difficulty thresholds, question and skill selection. Pure functions, no I/O. | Yes, unchanged |
| `store/` | `Store` interface + in-memory implementation and JSON loader | Interface yes; add a Postgres implementation |
| `practice/` | The flow: next question, grade, update mastery, model parameters, CSV export | Yes, unchanged |
| `api/` | Gin handlers for `/api/v1/practice/*` and `/api/v1/model/*` | Yes; swap `RequireUser` for `middleware.RequireAuth()` |
| `fit/`, `tools/` | Runs pyBKT on the answer log and loads fitted parameters | Yes, as the scheduled offline job |
| `sim/`, `cmd/simulate` | Simulated students and pyBKT CSV export | Stays as a tool |
| `web/`, `cmd/demo`, `data/` | Demo page, server, sample questions | No |

## API

All practice routes need an `X-User-ID` header (the backend uses its JWT instead). Responses use the backend's `{"success": true, "data": …}` envelope.

| Method | Path | Body / query | Returns |
| --- | --- | --- | --- |
| `GET` | `/api/v1/subjects` | | Subjects and their units |
| `POST` | `/api/v1/practice/next` | `{"subject_id": "chemistry", "skill": "Chemical Bonding"}` (`skill` optional) | Next question (no answer), current mastery, target difficulty, `retry` flag, or `done` |
| `POST` | `/api/v1/practice/answer` | `{"question_id": 201, "selected_option": "a"}` | `is_correct`, `correct_option`, explanation, `mastery_before`, `mastery_after`, `mastered`, `just_mastered`, and `next` |
| `GET` | `/api/v1/practice/mastery?subject_id=chemistry` | | Mastery, attempts and target difficulty per unit |
| `POST` | `/api/v1/practice/reset` | | Clears the student's state (demo only) |
| `GET` | `/api/v1/practice/export.csv` | | All answers as `order_id,user_id,skill_name,correct,subject` for pyBKT |
| `GET` | `/api/v1/model/params?subject_id=chemistry` | | Prior, learn, guess, slip, source and answers per unit |
| `PUT` | `/api/v1/model/params` | `{"source": "fitted", "params": [{"subject", "skill", "prior", "learn", "guess", "slip"}]}` | Loads parameters fitted elsewhere; all or nothing |
| `POST` | `/api/v1/model/fit` | | Runs pyBKT now and loads the results (503 if Python or pyBKT is missing) |
| `POST` | `/api/v1/model/simulate` | `{"subject_id": "chemistry", "students": 150, "steps": 20}` | Adds simulated history (demo only) |

The `/model` routes have no auth in the MVP; in LearnSteer they belong behind an admin role check.

Errors: unknown subject, skill or question returns 404; an invalid option or invalid parameters return 400; answering a question again returns 409 unless it is an allowed retry (last answer wrong, 3+ answers ago).

## Cold start

No answer history is needed. The engine starts every unit with hand-set parameters (prior 0.30, learn 0.10, guess 0.20, slip 0.10), so it adapts from the first answer. Once enough real answers exist, a pyBKT fit replaces them per unit; units without enough data keep the defaults.

## Plugging into learnsteer-backend

1. Copy `engine/`, `practice/` and `fit/` into `internal/adaptive/` (or import this module).
2. Add `store/postgres.go` implementing `Store` with GORM over `questions` (+ `difficulty`), `skill_params`, `student_skill_mastery` and `practice_attempts`. The schema is in the implementation doc.
3. Run the fit as a scheduled job (e.g. monthly `cmd/tools/fit_bkt` using `fit.Runner`) rather than from a web request.
4. Register the handler on the protected group in `cmd/server/main.go`, using `RequireAuth()` and the JWT user ID instead of `X-User-ID`.
5. Point the React practice page's `VITE_API_BASE_URL` at the backend instead of this demo.

## MVP limits

- Everything is in memory; restarting the demo clears all progress and fitted values.
- Skills are units (`unit_name`), trimmed of extra spaces; finer topics need content tagging.
- Classic BKT: no forgetting, and the difficulty tag chooses which question to serve but does not change the guess or slip numbers.
- Retries count as normal practice opportunities, although the student has seen the explanation.

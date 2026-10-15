# trim-1-demo

A local page for seeing what [trim-1](https://huggingface.co/caveman-so/trim-1) does with a code repository. You type a question, and the page shows which lines trim-1 scored highest, which files those lines rank first, and the compressed text trim-1 hands back. Every run gets saved as a JSON trace.

trim-1 is a 1.2 MB gradient-boosted tree model that scores each line of a text for how likely it is needed to answer a query. It runs on the CPU in milliseconds with no GPU and no network. It is sold as context compression for coding agents. This demo uses it as a retriever: the whole repository goes in as one text, and the line scores tell you where to look.

## Install

Go 1.24 or newer. That's it.

## Run

```bash
go run . -ext .py,.cpp,.cu,.md ~/Documents/ColBERT
```

Then open http://127.0.0.1:8010. The first version of the page, with file rankings and the compressed output, is at http://127.0.0.1:8010/v1.

The page also works as a static site (GitHub Pages), where it can only load the saved traces. Running a new query needs the server.

- `-ext` limits the search to those file extensions. Leave it out to search every git-tracked text file, but bundled docs and JavaScript (like `jquery.js`) push the real code down the ranking.
- `-port` or `PORT` changes the port (default 8010).

## What the page shows

- **The raw output.** Every line of the joined text with its trim-1 score, which is all `trim.Score` returns. Click **Score** to sort highest first, then lowest first, then back to line order.
- **trim-1 Rank.** Each line's place when all lines are sorted by score (1 = highest).
- **Opus Rank and the yellow rows.** For queries in `picks.json`, the 5 lines Claude Opus picked as most needed to answer the query, in order of importance. Click **Opus Rank** to bring them to the top. The note above the table lists their trim-1 ranks.

The `/v1` page shows:

- **Files, ranked by their best line.** A file's score is its highest-scoring line. trim-1 doesn't rank files itself; this is how the demo turns line scores into a file ranking.
- **Lines, ranked by score.** The top 30 lines with their file and line number.
- **Output.** What `trim.Compress` returns: the kept lines, unchanged and in order, with a `… N lines …` marker for each removed run.
- **Setting.** `conservative`, `balanced` (the default) or `aggressive`. Each one keeps every line scoring at least some share of the best line's score (0.35%, 0.8%, 3.5%), plus the top 3 lines, 1 line of context around each, and their enclosing headers. The setting changes the output but not the scores or the rankings.

## Stuff to know

- All the files go into one context with a `==> path <==` line before each file. trim-1 treats that as the start of a section.
- Every run saves to `traces/<date>/<time>-<query>.json`. The dropdown at the top loads any of them back. `review_notes` is there to fill in by hand.
- `traces/index.json` lists every trace, newest first. The server rewrites it after each run, and the static page reads it.
- `picks.json` maps a query to the lines picked for it, as `[path, line]` pairs. A run of a query in `picks.json` saves those lines in its trace as `reference_lines`.
- The saved traces search [ColBERT](https://github.com/stanford-futuredata/ColBERT) at commit `cc4f3dc`, so they contain its code. ColBERT is MIT-licensed, copyright Stanford Future Data Systems.
- One question runs at a time, so the timings aren't shared with another run.

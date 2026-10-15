# trim-1-demo

A local page for seeing what [trim-1](https://huggingface.co/caveman-so/trim-1) does with a code repository. You type a question, and the page shows which lines trim-1 scored highest, which files those lines rank first, and the compressed text trim-1 hands back. Every run gets saved as a JSON trace.

trim-1 is a 1.2 MB gradient-boosted tree model that scores each line of a text for how likely it is needed to answer a query. It runs on the CPU in milliseconds with no GPU and no network. It is sold as context compression for coding agents. This demo uses it as a retriever: the whole repository goes in as one text, and the line scores tell you where to look.

## Install

Go 1.24 or newer. That's it.

## Run

```bash
go run . -ext .py,.cpp,.cu,.md ~/Documents/ColBERT
```

Then open http://127.0.0.1:8010.

- `-ext` limits the search to those file extensions. Leave it out to search every git-tracked text file, but bundled docs and JavaScript (like `jquery.js`) push the real code down the ranking.
- `-port` or `PORT` changes the port (default 8010).

## What the page shows

- **Files, ranked by their best line.** A file's score is its highest-scoring line. trim-1 doesn't rank files itself; this is how the demo turns line scores into a file ranking.
- **Lines, ranked by score.** The top 30 lines with their file and line number.
- **Output.** What `trim.Compress` returns: the kept lines, unchanged and in order, with a `… N lines …` marker for each removed run.
- **Setting.** `conservative`, `balanced` (the default) or `aggressive`. Each one keeps every line scoring at least some share of the best line's score (0.35%, 0.8%, 3.5%), plus the top 3 lines, 1 line of context around each, and their enclosing headers. The setting changes the output but not the scores or the rankings.

## Stuff to know

- All the files go into one context with a `==> path <==` line before each file. trim-1 treats that as the start of a section.
- Every run saves to `traces/<date>/<time>-<query>.json`. The dropdown at the top loads any of them back. `review_notes` is there to fill in by hand.
- `traces/` is gitignored, because traces contain code from whatever repo you ask about.
- One question runs at a time, so the timings aren't shared with another run.

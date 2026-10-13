# 17 and 18 Contest Validation Notes

This file is the durable record for validating historical submissions. It is
updated when a statement, reference algorithm, resource limit, or suspicious
verdict is checked.

## Validation Method

- The normalized `testdata` inputs and outputs are the current test oracle.
- A reference solution must pass every stored case before submissions for that
  problem are judged.
- A verdict that looks suspicious is checked against the statement, the
  algorithm below, and its first failing case. A test result alone is not
  treated as proof.
- The historical Python JustJudge may be compared later, but it is secondary:
  its compiler, operating system, and resource measurement differ from this
  engine.

## Resource Limits

### Contest 17

The recovered statement PDF contains no per-language time or memory limits.
Until they are recovered, batch validation uses the explicit temporary limit
of 2 seconds CPU time and 512 MiB memory for every language. This is not a
claim about the original contest limits.

### Contest 18

The statement specifies 1 second / 256 MiB for C++, 1.5 seconds / 256 MiB for
Java, and 2 seconds / 512 MiB for Python. Each problem is worth 20 points.
The batch runner must apply these language-specific limits.

## Contest 17

### A: Implement It! My Dream!

- Task: read `n` and print `Bloom with JustCPC,Bloom with dream!` exactly `n`
  times.
- Reference algorithm: direct loop, `O(n)`.
- Validation focus: the comma and exclamation mark are ASCII; output count
  must equal `n`.

### B: The Game of Choosing a Contest Site

- Task: Taffy may choose one of six sites. Her rank at a site is one plus the
  number of registered teams there whose strength is strictly greater than
  hers. Output the best possible rank.
- Reference algorithm: start each of the six ranks at one; scan all teams and
  increment that site's count iff `x > m`; return the minimum. `O(n)` time,
  `O(1)` memory.
- Validation focus: equal strength does not outrank Taffy, and an empty site
  gives rank one.

### C: Twilight Tavern

- Task: process customers in nondecreasing arrival-time order. A customer at
  time `a` using seat `p` for `t` minutes succeeds iff the seat is free; a
  successful customer pays `w`. Output the smallest seat index with maximum
  accumulated tip.
- Reference algorithm: `free_at[p]` stores the first free minute. Accept iff
  `free_at[p] <= a`, then set it to `a + t` and add the tip. Scan totals at
  the end. `O(n + m)` time, `O(n)` memory.
- Validation focus: a customer occupying minutes `[a, a+t-1]` leaves at
  `a+t`; equality at the next arrival is accepted. Ties go to the smaller
  seat index.

### D: The Last Supper

- Task: people sit in a ring. `s[i]` says whether person `i` calls person
  `i+1` bad. Good people tell the truth; bad people invert it. Find the
  largest possible number of bad people, or `-1` if the statements contradict.
- Reference algorithm: let `x[i]` mean that person `i` is bad. Then
  `s[i] = x[i] XOR x[i+1]`. Choose `x[1]` as zero and propagate around the
  ring. A solution exists iff the closing value equals the chosen `x[1]`;
  the only other solution is its complement. Output the larger of the two bad
  counts. `O(n)` time, `O(n)` or `O(1)` memory.
- Validation focus: an odd number of `1`s makes the XOR cycle inconsistent;
  both complementary assignments must be considered.

### E: Love and Concerts: The Superstar

- Task: each selected concert costs the same preparation time `t` and earns a
  positive value. For each target `m`, reach at least that value using the fewest
  concerts, or report `-1`.
- Reference algorithm: sort values descending, make prefix sums, and binary
  search the first prefix at least `m`. Answer `count * t`. `O(n log n + q log
  n)` time, `O(n)` memory.
- Validation focus: the target may be exceeded; return `-1` only when the sum
  of all values is too small.

### F: Magical Set

- Task: initially empty set `S`; insertion adds every integer in `[L,R]`.
  Query whether `gcd(S) == POSmex(S)`, where POSmex is the least missing
  positive integer.
- Reference algorithm: if any inserted interval starts at one, then `1` is in
  `S` and equality can never hold. Any non-singleton interval contains two
  consecutive integers, making the overall gcd one. Otherwise maintain the
  gcd of singleton insertions. Query is `Yes` exactly when one has not appeared
  and that gcd is one. `O(Q log V)` time, `O(1)` memory.
- Validation focus: `[1,1]` and `[1,R]` both contain one; `L < R` immediately
  forces gcd one.

### G: More Love

- Task: at each round choose either card A, scoring `a[i]`, or card B, making
  the current and future round score at least `b[i]` until replaced by another
  B card. Maximize total score.
- Reference algorithm: choose the rounds at which B cards are played. Between
  two chosen B cards, all A choices score `max(a[j], current_b)`. A virtual
  terminal B with floor zero closes the final segment. The reference reverses
  indices, precomputes segment sums, then applies a partition DP over the last
  chosen B card. `O(n^2)` time and memory for `n <= 5000`.
- Validation focus: B scores on the round it is played, replaces rather than
  combines with the previous B, and a strategy with no real B card remains
  possible through the virtual terminal state.

### H: Polymino Mystery

- Task: count nonempty connected black-cell subsets of a `2 x n` grid modulo
  `998244353`, with `n <= 10^18`.
- Expected algorithm: finite-state matrix exponentiation / linear recurrence
  over column occupancy states, logarithmic in `n`.
- Scope: this problem is deliberately excluded from the 17th-contest batch.
  The historical contest had no valid submission and the normalized test set
  contains only A-G; no reference solution was supplied.

## Contest 18

### A: Ultimate Password of an Interstellar Relic

- Task: no input; output `WYCWSFGS`.
- Reference algorithm: print the literal.
- Validation focus: uppercase spelling and no extra prompt text.

### B: Poor Little Penguins

- Task: every day all living penguins attack, then the bear eats one. Choose
  the eating order that minimizes the total damage.
- Reference algorithm: eat in nonincreasing attack order. Sorting ascending
  and summing suffix sums gives the same result. `O(n log n)` time.
- Validation focus: the penguin eaten at the end of a day still attacks that
  day; equal attack values are interchangeable.

### C: The Most Promising Year

- Task: a query at position `i` reveals only the two teams' prefix score after
  game `i`. Find the minimum number of queries needed to reconstruct the full
  A/B winner sequence.
- Reference algorithm: between two queried positions, the exact order is
  determined only if that substring is monochromatic. Thus query once per
  maximal equal-character run. Count adjacent changes plus one. `O(n)` time.
- Validation focus: a length-one string needs one query; alternating winners
  need `n` queries.

### D: Favorite Icebreaker Activity

- Task: `a` is `[1] + [1,2] + ... + [1,...,n]`. Count occurrences of a
  guaranteed-present sequence `b` of length `m` in `a`.
- Reference algorithm: if `b` ever decreases, it crosses a unique block
  boundary and occurs exactly once. Otherwise it is a consecutive increasing
  sequence ending at `b[m]`, and it occurs in every block with length at least
  that value: `n - b[m] + 1`. `O(m)` time.
- Validation focus: `m = 1`, a decrease exactly at a block boundary, and the
  fact that the statement guarantees at least one occurrence.

### E: Bees Communicating with Each Other

- Task: count unordered pairs of bee positions whose distance is at most `d`.
- Reference algorithm: sort coordinates, advance a right pointer for each left
  pointer, and add the current window size. `O(n log n)` time, `O(n)` memory.
- Validation focus: equality at distance `d` counts; the answer needs 64 bits.

### F: Crossing the Cave

- Task: from room `A`, move from room `i` to `i +/- K[i]` if still in
  `[1,N]`; find the fewest moves to room `B`.
- Reference algorithm: BFS on the implicit graph. `O(N)` time and memory.
- Validation focus: `A == B`, zero jumps, invalid destinations, and marking a
  room visited when it is enqueued.

### G: Brawlhalla

- Task: characters are numbered row-major in rows of width `m`; the final row
  may be partial. Arrow moves wrap across edges using the displayed grid rules.
  Find the minimum key presses from character one to character `k` for up to
  `10^5` cases with values up to `10^9`.
- Reference algorithm: convert `k` to row and column, derive the two possible
  wrap-aware distances from the first-cell endpoint and the partial-last-row
  endpoint, and take the minimum. `O(1)` per case.
- Validation focus: `k` at a row end, `n mod m == 0`, a partial final row, and
  wrapping through columns absent from that final row.

### H: Tree Guard Battle

- Task: a thief starts at root one. Each turn police blocks a non-thief node,
  then the thief moves or stays; reaching a non-root leaf lets the thief win.
  Decide whether police has a forced win.
- Reference algorithm: root the tree at one. A non-root leaf is an escaping
  branch; a non-root degree-two node is a forced corridor and contributes no
  branching choice. DFS aggregates whether each node has more than one
  effective escaping child branch. Police wins iff the root has at most one;
  this is exactly the reference's degree-based recurrence. `O(n)` time and
  memory across each test case.
- Validation focus: root degree is not treated as a non-root leaf/corridor,
  and every test case must reset graph and visitation state.

## Result Record

- 2026-07-29: all 15 supplied reference solutions pass all 196 normalized
  cases using the current judge engine. The previous temporary uniform limit
  was 2 seconds CPU time and 512 MiB memory.
- 2026-07-29: contest statements recovered from
  `比赛数据/17届比赛数据/第十七届江苏科技大学程序设计竞赛(正式赛).pdf` and
  `比赛数据/18届比赛数据/第十八届江苏科技大学程序设计竞赛赛题.pdf`.
- 2026-07-29: batch-tested all 1,611 normalized historical submissions through
  the current engine's queue with at most 10 simultaneous tasks. The report is
  `test/results/historical-submissions.json` and is deliberately Git-ignored.
  It contains only verdicts, per-case measurements, and source paths; it does
  not duplicate source code, test data, or program output.
- 2026-07-29: the batch executed 9,330 stored test points for 908 contest-17
  submissions and 9,474 for 703 contest-18 submissions. No submission produced
  a `SYS` verdict. Contest 17 continues to use the temporary 2 second / 512 MiB
  limit because its original limit metadata is unavailable.
- 2026-07-29: `JustJudge/CodeEvalutor.py` was run directly against the current
  normalized 18th-contest data for every historical-score discrepancy. Inputs
  and answers were explicitly paired in numeric order, so the comparison does
  not inherit the old `main.py` unordered-directory bug. The structured result
  is `test/results/legacy-run-current-cases-18.json`.
- 2026-07-29: the legacy runner and new engine had equal AC-case counts for 15
  of those 24 submissions and differed for 9. The legacy runner still cannot
  be an oracle: it treats a signal exit with no stderr as normal completion and
  then WA on empty output, measures wall time, and samples only the launched
  process's RSS.

### Detailed 18th-Contest Legacy Runner Comparison

The following table uses the old `CodeRunner` with the **current** normalized
test data. Counts are accepted cases over saved cases, not the historical CSV
score. `same` means equal accepted-case count; verdict labels may still differ
because the old runner often converts a crash with empty stderr into WA.

| User ID | Problem / language | Legacy cases | New cases | Result | Checked reason |
| --- | --- | ---: | ---: | --- | --- |
| 232210709116 | E / C++ | 4/10 | 4/10 | same | Cases 5-10: legacy WA, new RE; both have the same passing points. |
| 232241807227 | E / C++ | 4/10 | 4/10 | same | Cases 5-10: legacy WA, new RE; both have the same passing points. |
| 242210714218 | E / C++ | 3/10 | 3/10 | same | Every case verdict agrees. |
| 242210788206 | E / C++ | 1/10 | 0/10 | different | Reads and writes `pos[1..n]` in a vector sized `n`; old case 1 AC is undefined behavior, new case 1 RE. |
| 242210788232 | E / C++ | 1/10 | 1/10 | same | Same passing count; later legacy WA differs from new TLE/RE classification. |
| 242210788411 | E / C++ | 4/10 | 4/10 | same | Every case verdict agrees. |
| 242210788432 | E / C++ | 2/10 | 1/10 | different | The `while (maxpos >= a[e+1])` loop has no end bound; old case 2 AC is undefined behavior, new case 2 WA. |
| 242212188525 | E / C++ | 2/10 | 2/10 | same | Same passing count; legacy calls later crashes WA, new calls them RE. |
| 251210701333 | E / C++ | 4/10 | 4/10 | same | Every case verdict agrees. |
| 252210488134 | E / Python | 4/10 | 4/10 | same | Every case verdict agrees. |
| 252210701629 | E / C++ | 2/10 | 2/10 | same | Same passing count; later legacy WA differs from new TLE. |
| 252210709511 | E / C++ | 1/10 | 1/10 | same | Same passing count; legacy calls later crashes WA, new calls them RE. |
| 242240788716 | A / C++ | 0/1 | 0/1 | same | Both compilers report CE for `#include <String>`. |
| 242210714218 | B / C++ | 0/10 | 0/10 | same | `acm` is uninitialized; both runs reject every case. |
| 242210788710 | B / C++ | 6/10 | 4/10 | different | Reads `a[n]` through `a[j+1]`; pass count varies with undefined behavior. |
| 252210710215 | B / C++ | 4/10 | 5/10 | different | Reads `a[n]` through `a[j+1]`; pass count varies with undefined behavior. |
| 252210710222 | B / C++ | 9/10 | 10/10 | different | Reads `p[n]`; legacy case 1 emits heap-corruption stderr, new run happens to pass all cases. |
| 251210701208 | D / C++ | 1/20 | 0/20 | different | Quadratic materialization exceeds the intended bounds; allocation behavior differs across runners. |
| 252210701223 | D / C++ | 3/20 | 3/20 | same | Every case verdict agrees. |
| 252210701232 | D / Python | 0/20 | 0/20 | same | Every case verdict agrees. |
| 252210709132 | D / C++ | 2/20 | 5/20 | different | Fixed 30-element arrays and assignment in the comparison create undefined behavior. |
| 232241807227 | F / C++ | 0/20 | 20/20 | different | Correct BFS declares two 2,000,006-element `long long` arrays on stack. Legacy default stack overflows, emits no stderr, and mislabels empty output WA; new stack limit is 64 MiB and accepts it. |
| 242210788232 | F / C++ | 2/20 | 2/20 | same | Always prints `-1`; cases 1 and 2 legitimately answer `-1`. |
| 242210788736 | G / Java | 0/20 | 8/20 | different | Legacy environment has no `javac`, so it gives CE. New Java toolchain compiles it; its full-row assumption then fails from case 9. |

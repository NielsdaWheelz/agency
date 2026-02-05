# issue process

## label taxonomy

priority (exactly one):
- `p0` critical correctness, data loss, unsafe deletes, contract breaks
- `p1` high: correctness/reliability/required feature gaps
- `p2` medium: maintainability, testability, perf, hygiene
- `p3` low: polish, docs, UX nits

type (exactly one):
- `type:bug`
- `type:tech-debt`
- `type:refactor`
- `type:enhancement`
- `type:design`
- `type:policy`
- `type:test`
- `type:docs`
- `type:chore`

area (exactly one, use only what’s relevant):
- `area:archive`
- `area:build`
- `area:checkpoint`
- `area:clean`
- `area:cli`
- `area:config`
- `area:core`
- `area:daemon`
- `area:daemonclient`
- `area:errors`
- `area:events`
- `area:exec`
- `area:git`
- `area:ids`
- `area:integration-worktree`
- `area:invocation`
- `area:landing`
- `area:lock`
- `area:merge`
- `area:paths`
- `area:pipeline`
- `area:product`
- `area:push`
- `area:render`
- `area:repo`
- `area:runservice`
- `area:runnerstatus`
- `area:runtime`
- `area:spec`
- `area:store`
- `area:stream`
- `area:tmux`
- `area:verify`
- `area:verifyservice`
- `area:watchdog`
- `area:worktree`
- `area:policy`
- `area:docs`

status (optional, one or more):
- `status:blocked`
- `status:needs-design`
- `status:needs-spec`
- `status:ready`
- `status:in-progress`

## milestone plan

- `stability-p0`
  all `p0` issues. must be empty before any release.

- `hardening-p1`
  top `p1` issues that impact correctness or required features.

- `quality-p2`
  maintainability/perf/testability items.

- `polish-p3`
  docs/ux cleanup.

rules:
1. every issue gets exactly one `p*`, one `type:*`, and one `area:*` label.
2. `p0` issues must be added to `stability-p0` immediately.
3. `p1` issues should target `hardening-p1` unless explicitly deferred.
4. close or split any issue that can’t be defined with acceptance criteria.

# Report cs0 — pin-gc-eaac364ee2fd

The ticket branch was already at the 22c9d3aaa2 pin, plus the fix in cc76a9b6850: an activated Support ability announces its target at the activate step. The bump branch wt/gc-pin-eaac364ee2fd was cut from an older develop, so the merge conflicted on GORGE_PIN, go.mod and go.sum. I took the bump branch's side for all three. The merge commit is 91b8326c959.

- GORGE_PIN is eaac364ee2fd8b8ca304ea0e939bba0636964121.
- go.mod requires gorge v0.0.0-20261125174106-eaac364ee2fd.
- No code changes were needed. The gate failure (TestSupportTargetTiming/activated_ability, "unconsumed answer ... choose Grizzly Bears") came from the old test expectations at the bump commit. They were already fixed on develop by cc76a9b6850.

Commands run:
- `make test-one PKG=./oracle/compliance/oraclegen RUN='^TestSupportTargetTiming$'` → PASS
- `make test-one PKG=./oracle/compliance/oraclegen RUN='^(TestSupport|TestChooseTargets|TestXAnswers)'` → PASS
- `make test-smart` → `ENV 91b8326c95 ./... 43s`, exit 3. Because the diff changes a pin, it expands to all 40 packages, and the output carried no test failure. I did not run the other packages.

## Issues
None.
